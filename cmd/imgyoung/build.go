// ImgYoung 组合根:按 基础设施 → 业务服务 → HTTP 三段显式装配对象图。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/core/alert"
	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/dedup"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/referer"
	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/core/replication"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/core/usage"
	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/http/router"
	"github.com/x-haose/imgyoung/internal/http/s3api"
	"github.com/x-haose/imgyoung/internal/infra/cdn"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/observability"
	"github.com/x-haose/imgyoung/internal/infra/secret"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
	"github.com/x-haose/imgyoung/web"
)

// readHeaderTimeout 读取请求头的上限,防慢速连接占用。
const readHeaderTimeout = 10 * time.Second

// urlFetchTimeout URL 转存单次抓取的总时长上限;抓取期间占着上传并发闸,不能由远端拖住。
const urlFetchTimeout = 30 * time.Second

// idleTimeout keep-alive 空闲连接的回收上限;无它则慢速读客户端可长期占用 goroutine
// (访问路径无并发闸兜底)。上传/下载本体不设 WriteTimeout,大图过慢链路由此放宽。
const idleTimeout = 120 * time.Second

// bytesPerGB 一 GB 的字节数(渠道配额阈值换算)。
const bytesPerGB = 1 << 30

// webhookTimeout 告警 Webhook 单次投递的超时;投递失败由下一轮评估重试。
const webhookTimeout = 10 * time.Second

// queueStatsTimeout 指标 scrape 时统计任务队列深度的查询超时。
const queueStatsTimeout = 2 * time.Second

// app 运行期对象图:基础设施、业务服务与 HTTP 服务器;构造在 build,启停在 serve。
type app struct {
	infra                  // 基础设施构件
	services               // 业务服务集
	logger   *slog.Logger  // 结构化日志
	httpSrv  *http.Server  // HTTP 服务器
	stop     chan struct{} // 关闭即请求整个进程优雅停机(恢复快照后由进程管理器拉起换入)
	stopOnce sync.Once     // 保证 stop 只关闭一次
}

// requestShutdown 请求优雅停机,可重复调用。
func (a *app) requestShutdown() {
	a.stopOnce.Do(func() { close(a.stop) })
}

// infra 聚合基础设施层构件,供业务服务与 HTTP 层取用;由 buildInfra 一次性装配。
type infra struct {
	boot    config.Bootstrap  // 引导配置,监听地址与数据目录等
	conn    *db.DB            // 元数据库句柄,全局唯一;写单连接、读连接池
	queries *sqlc.Queries     // 类型安全查询集
	master  *secret.MasterKey // 主密钥,渠道配置与加密数据的根
	store   *config.Store     // 运营配置存储,各处逐轮取快照
}

// services 聚合全部业务服务;带后台循环的由 serve 启停,其余供 HTTP 装配取用。
type services struct {
	auth     *auth.Service          // 认证服务
	manager  *channel.Manager       // 渠道管理器
	runner   *replication.Runner    // 补齐子系统
	referer  *referer.Service       // 防盗链白名单
	metrics  *observability.Metrics // 指标集
	audit    *audit.Recorder        // 审计记录器
	alerts   *alert.Service         // 告警评估与投递
	vstore   *variants.Store        // 衍生图磁盘缓存(访问时间周期落库)
	variants *variants.Service      // 按需处理服务
	upload   *upload.Service        // 上传服务
	tracker  *lifecycle.Tracker     // 访问活跃度追踪器
	sweeper  *lifecycle.Sweeper     // 生命周期清理器
	merger   *dedup.Merger          // 后台查重执行器
	reporter *usage.Reporter        // 渠道用量上报器
	backup   *reliability.Service   // 元数据库备份服务
}

// newLogger 构造进程级结构化日志,并设为 slog 默认。
// @return 结构化日志
func newLogger() *slog.Logger {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	return logger
}

// build 组合根:分 基础设施 → 业务服务 → HTTP 三段显式装配,任一步失败即返回。
// @Description: 成功返回可运行的 app 与清理函数(先关渠道驱动、再关数据库;供调用方 defer,
// 覆盖装配失败与正常退出两条路径);基础设施装配成功后、后续任一步失败,由本函数的 built 闸
// 触发基础设施清理。
// @param logger 结构化日志
// @return 运行期组件
// @return 清理函数(非 nil 即须调用)
// @return 装配失败时的错误
func build(logger *slog.Logger) (*app, func(), error) {
	in, closeInfra, err := buildInfra(logger)
	if err != nil {
		return nil, nil, err
	}
	// 基础设施已开库;业务服务装配任一步失败即关连接,不泄漏。
	built := false
	defer func() {
		if !built {
			closeInfra()
		}
	}()

	svcs, err := buildServices(in, logger)
	if err != nil {
		return nil, nil, err
	}

	built = true
	cleanup := func() {
		if cerr := svcs.manager.Close(context.Background()); cerr != nil {
			logger.Error("关闭渠道驱动失败", "err", cerr)
		}
		closeInfra()
	}
	a := &app{infra: in, services: svcs, logger: logger, stop: make(chan struct{})}
	a.httpSrv = buildHTTP(in, svcs, a.requestShutdown, logger)
	return a, cleanup, nil
}

// buildInfra 装配基础设施:建数据目录 → 开库 → 迁移 → 主密钥 → 运营配置存储。
// @Description: 内部自持 ok 闸,装配任一步失败即关闭已开的库;成功则把清理函数返给
// 调用方(供其覆盖后续装配失败与正常退出)。
// @param logger 结构化日志
// @return 基础设施构件
// @return 数据库清理函数(仅成功时非 nil)
// @return 装配失败时的错误
func buildInfra(logger *slog.Logger) (infra, func(), error) {
	boot, err := config.LoadBootstrap()
	if err != nil {
		return infra{}, nil, fmt.Errorf("引导配置非法: %w", err)
	}
	if err := os.MkdirAll(boot.DataDir, 0o750); err != nil {
		return infra{}, nil, fmt.Errorf("创建数据目录 %q: %w", boot.DataDir, err)
	}

	// 开库之前换入暂存的恢复快照:全部句柄都指着库文件,只有这个时刻能安全替换。
	if _, err := reliability.ApplyStaged(boot.DataDir, logger); err != nil {
		return infra{}, nil, fmt.Errorf("换入恢复快照: %w", err)
	}
	conn, err := db.Open(filepath.Join(boot.DataDir, reliability.DBFileName))
	if err != nil {
		return infra{}, nil, err
	}
	cleanup := func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Error("关闭数据库失败", "err", cerr)
		}
	}
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()

	if err := db.Migrate(conn); err != nil {
		return infra{}, nil, err
	}

	queries := sqlc.New(conn)
	master, err := loadMasterKey(boot, logger)
	if err != nil {
		return infra{}, nil, err
	}
	store, err := loadSettings(conn, queries, master, logger)
	if err != nil {
		return infra{}, nil, err
	}

	ok = true
	return infra{boot: boot, conn: conn, queries: queries, master: master, store: store}, cleanup, nil
}

// buildServices 装配全部业务服务:先建被复用且可失败的图内节点(认证/渠道/补齐/防盗链/指标/审计/
// 衍生图缓存),再拼出依赖它们的数据面服务与后台循环。
// @Description: 首启在此播种管理员密码;metrics 挂在 manager 上、供 reporter 与 router 共用。
// @param in 基础设施构件
// @param logger 结构化日志
// @return 业务服务集
// @return 装配失败时的错误
func buildServices(in infra, logger *slog.Logger) (services, error) {
	writer := in.conn.Writer()
	s := services{
		auth:  auth.New(auth.Config{Conn: writer, Queries: in.queries, Master: in.master, Logger: logger}),
		audit: audit.New(in.queries),
	}
	if err := seedAdminPassword(in.queries, logger); err != nil {
		return services{}, err
	}
	manager, err := channel.NewManager(context.Background(), channel.ManagerConfig{
		Conn: writer, Queries: in.queries, Master: in.master, DataDir: in.boot.DataDir, Logger: logger,
	})
	if err != nil {
		return services{}, err
	}
	s.manager = manager
	s.metrics = buildMetrics(manager)
	s.runner, err = replication.NewRunner(replication.RunnerConfig{
		Conn: writer, Queries: in.queries, Manager: manager, Metrics: s.metrics,
		Purger: settingsPurger{settings: in.store.Current}, Logger: logger,
	})
	if err != nil {
		return services{}, err
	}
	registerQueueGauges(s.metrics, s.runner, logger)
	s.referer, err = referer.New(context.Background(), in.queries)
	if err != nil {
		return services{}, err
	}
	s.vstore = variants.New(variants.Config{
		Dir:      filepath.Join(in.boot.DataDir, "variants"),
		Capacity: func() int64 { return in.store.Current().VariantsCacheMB << 20 },
		Queries:  in.queries, Logger: logger,
	})
	s.buildDataServices(in, logger)
	return s, nil
}

// buildDataServices 装配依赖图内节点的数据面服务与后台循环:按需处理、上传、活跃度、清理、查重、用量、备份。
// @param in 基础设施构件
// @param logger 结构化日志
func (s *services) buildDataServices(in infra, logger *slog.Logger) {
	writer := in.conn.Writer()
	s.variants = variants.NewService(variants.ServiceConfig{
		Store: s.vstore, Settings: in.store.Current, Metrics: s.metrics, Logger: logger,
	})
	s.upload = upload.New(upload.Config{
		Conn: writer, Queries: in.queries, Channels: s.manager, Replicator: s.runner,
		Settings: in.store.Current, Metrics: s.metrics, Logger: logger,
	})
	s.tracker = lifecycle.NewTracker(writer, in.queries,
		func() bool { return in.store.Current().TTLRenewOnAccess }, logger)
	s.sweeper = lifecycle.New(lifecycle.Config{
		Conn: writer, Queries: in.queries, Deleter: s.runner, Variants: s.vstore, Purger: s.runner,
		Settings: in.store.Current, Logger: logger,
	})
	s.merger = dedup.New(dedup.Config{Conn: writer, Queries: in.queries, Settings: in.store.Current, Logger: logger})
	s.reporter = usage.New(usage.Config{
		Queries: in.queries, Gauge: s.metrics,
		QuotaBytes: func() int64 { return int64(in.store.Current().ChannelQuotaGB) * bytesPerGB },
		Logger:     logger,
	})
	s.backup = reliability.New(reliability.Config{
		Conn: in.conn.Reader(), Channels: s.manager, DataDir: in.boot.DataDir,
		Keep:     func() int { return in.store.Current().BackupKeep },
		Interval: func() time.Duration { return time.Duration(in.store.Current().BackupIntervalHours) * time.Hour },
		Logger:   logger,
	})
	s.alerts = alert.New(alert.Config{
		Queries:    in.queries,
		QuotaBytes: func() int64 { return int64(in.store.Current().ChannelQuotaGB) * bytesPerGB },
		Backup:     s.backup,
		WebhookURL: func() string { return in.store.Current().AlertWebhookURL },
		Client:     &http.Client{Timeout: webhookTimeout},
		Logger:     logger,
	})
}

// registerQueueGauges 挂任务队列深度量表:scrape 时查一次任务表;查询失败记日志并报 NaN(表示未知)。
// @param m 指标集
// @param runner 补齐子系统
// @param logger 结构化日志
func registerQueueGauges(m *observability.Metrics, runner *replication.Runner, logger *slog.Logger) {
	stats := func() replication.QueueStats {
		ctx, cancel := context.WithTimeout(context.Background(), queueStatsTimeout)
		defer cancel()
		st, err := runner.QueueStats(ctx)
		if err != nil {
			logger.Warn("统计任务队列深度失败", "err", err)
			return replication.QueueStats{Waiting: -1, Running: -1}
		}
		return st
	}
	gauge := func(v int64) float64 {
		if v < 0 {
			return math.NaN()
		}
		return float64(v)
	}
	m.RegisterGauge("queue_jobs_waiting", "任务队列里等待执行的任务数(可认领、已排程与待重试)。",
		func() float64 { return gauge(stats().Waiting) })
	m.RegisterGauge("queue_jobs_running", "任务队列里正在执行的任务数。",
		func() float64 { return gauge(stats().Running) })
}

// buildHTTP 装配 HTTP 层:公有 API、后台 API、路由,组装成配好超时的 http.Server。
// @param in 基础设施构件
// @param s 业务服务集
// @param shutdown 请求整个进程优雅停机的入口,恢复快照后调用
// @param logger 结构化日志
// @return HTTP 服务器
func buildHTTP(in infra, s services, shutdown func(), logger *slog.Logger) *http.Server {
	// URL 转存的大小上限与单图上限同源(逐次取快照,后台改即生效)。
	fetcher := urlfetch.New(func() int64 { return in.store.Current().MaxUploadBytes() }, urlFetchTimeout)
	pub := publicapi.New(publicapi.Config{
		Queries: in.queries, Channels: s.manager, Upload: s.upload, Fetch: fetcher,
		Settings: in.store.Current, Activity: s.tracker, Variants: s.variants, Logger: logger,
	})
	adm := adminapi.New(adminapi.Config{
		Admin: s.manager, OAuth: channel.NewOAuthSessions(),
		Replicator: s.runner, Auth: s.auth, Images: s.sweeper, Dedup: s.merger,
		Queries: in.queries, Audit: s.audit, Referer: s.referer, Settings: in.store, Boot: in.boot,
		Backups: s.backup, Shutdown: shutdown, Logger: logger,
	})
	s3 := s3api.New(s3api.Config{
		Auth: s.auth, Upload: s.upload, Queries: in.queries, Objects: pub, Images: s.sweeper, Audit: s.audit,
		Settings: in.store.Current, Logger: logger,
	})
	handler := router.New(router.Config{
		Public: pub, Admin: adm, S3: s3, Auth: s.auth, Settings: in.store.Current,
		RefererPatterns: s.referer.Patterns, Metrics: s.metrics,
		TrustedProxies: in.boot.TrustedProxies, MetricsAllow: in.boot.MetricsAllow,
		Web: web.Dist(), Logger: logger,
	})
	return &http.Server{
		Addr:              in.boot.Listen,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// loadMasterKey 解析主密钥:环境变量(hex)优先,否则数据目录文件(首启生成)。
// @return 主密钥
// @return 密钥非法或文件读写失败时的错误
func loadMasterKey(boot config.Bootstrap, logger *slog.Logger) (*secret.MasterKey, error) {
	if boot.MasterKey != "" {
		key, err := secret.MasterKeyFromHex(boot.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("master_key 配置非法: %w", err)
		}
		return key, nil
	}
	key, created, err := secret.LoadOrCreate(boot.DataDir)
	if err != nil {
		return nil, err
	}
	if created {
		logger.Warn("已生成主密钥文件,请立即备份——丢失将导致渠道配置与加密渠道数据永久不可恢复",
			"path", filepath.Join(boot.DataDir, "master.key"))
	}
	return key, nil
}

// loadSettings 装载运营配置存储,并打一行 CDN purge 启用状态便于自助排障。
// @return 配置存储
// @return 装载失败时的错误
func loadSettings(conn *db.DB, queries *sqlc.Queries, master *secret.MasterKey,
	logger *slog.Logger) (*config.Store, error) {
	store, err := config.LoadStore(context.Background(), conn.Writer(), queries, master)
	if err != nil {
		return nil, err
	}
	if set := store.Current(); set.CDNPurgeEnabled() {
		logger.Info("CDN purge 已启用(Cloudflare)", "zone", set.CDNCloudflareZoneID)
	} else {
		logger.Info("CDN purge 未启用(需在设置页同时配置 zone、api_token 与 base_url)")
	}
	return store, nil
}

// seedAdminPassword 首启把内置管理员播种为出厂默认密码,幂等。
// @Description: 仅当管理员尚未设密时播种 auth.DefaultAdminPassword 并置强制改密标记,
// 保证零配置开箱可登录;已设密的库不触碰。失败即启动失败,不以半初始化状态对外。
// @param queries 查询集
// @param logger 结构化日志
// @return 播种失败时的错误
func seedAdminPassword(queries *sqlc.Queries, logger *slog.Logger) error {
	ctx := context.Background()
	admin, err := queries.GetUser(ctx, sqlc.DefaultUserID)
	if err != nil {
		return fmt.Errorf("读取内置管理员: %w", err)
	}
	if admin.PasswordHash != "" {
		return nil
	}
	phc, err := auth.HashPassword(auth.DefaultAdminPassword)
	if err != nil {
		return fmt.Errorf("哈希默认管理员密码: %w", err)
	}
	if _, err := queries.SeedAdminPassword(ctx, sqlc.SeedAdminPasswordParams{
		PasswordHash: phc, ID: sqlc.DefaultUserID,
	}); err != nil {
		return fmt.Errorf("播种默认管理员密码: %w", err)
	}
	logger.Warn("已初始化默认管理员密码,请登录后立即在后台修改",
		"user", "admin", "password", auth.DefaultAdminPassword)
	return nil
}

// settingsPurger 按当前配置执行 CDN purge:逐次取快照判定启用与凭证,
// 后台改配置即刻生效;未启用时空操作成功,遗留任务安静消化。
type settingsPurger struct {
	settings func() config.Settings // 运营配置快照读取口
}

// Purge 清理给定绝对 URL 的边缘缓存;purge 未启用时为空操作。
// @param ctx 控制超时与取消
// @param urls 待清理的绝对 URL 列表
// @return Cloudflare API 错误
func (p settingsPurger) Purge(ctx context.Context, urls []string) error {
	set := p.settings()
	if !set.CDNPurgeEnabled() {
		return nil
	}
	return cdn.NewCloudflare(set.CDNCloudflareZoneID, set.CDNCloudflareAPIToken).Purge(ctx, urls)
}

// buildMetrics 构造指标集,并挂上读取渠道健康的量表(闭包依赖 core,留在装配根)。
// @return 指标集
func buildMetrics(manager *channel.Manager) *observability.Metrics {
	m := observability.New()
	m.RegisterGauge("channels_healthy_total", "当前健康渠道数。",
		func() float64 { return float64(len(manager.Healthy())) })
	m.RegisterGauge("channels_total", "已加载渠道总数(含不健康)。",
		func() float64 { return float64(len(manager.All())) })
	return m
}
