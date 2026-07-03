package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/dedup"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/core/replication"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/core/usage"
	"github.com/x-haose/imgyoung/internal/http/server"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/observability"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// readHeaderTimeout 读取请求头的上限,防慢速连接占用。
const readHeaderTimeout = 10 * time.Second

// fxErrorLogger 安静版 fxevent.Logger:只把带错误的事件写进 slog,略过逐条装配噪音。
// 保证生产日志干净,又绝不吞掉启动/停机错误(NopLogger 会吞,不可取)。
type fxErrorLogger struct {
	// log 项目结构化日志。
	log *slog.Logger
}

// LogEvent 仅在事件携带错误时记录,其余静默。
// @param e fx 生命周期事件
func (l fxErrorLogger) LogEvent(e fxevent.Event) {
	switch ev := e.(type) {
	case *fxevent.Provided:
		if ev.Err != nil {
			l.log.Error("fx 依赖注册失败", "err", ev.Err)
		}
	case *fxevent.Invoked:
		if ev.Err != nil {
			l.log.Error("fx 调用失败", "func", ev.FunctionName, "err", ev.Err)
		}
	case *fxevent.Started:
		if ev.Err != nil {
			l.log.Error("fx 启动失败", "err", ev.Err)
		}
	case *fxevent.Stopped:
		if ev.Err != nil {
			l.log.Error("fx 停止出错", "err", ev.Err)
		}
	case *fxevent.OnStartExecuted:
		if ev.Err != nil {
			l.log.Error("生命周期启动钩子失败", "callee", ev.FunctionName, "err", ev.Err)
		}
	case *fxevent.OnStopExecuted:
		if ev.Err != nil {
			l.log.Error("生命周期停止钩子失败", "callee", ev.FunctionName, "err", ev.Err)
		}
	case *fxevent.RolledBack:
		l.log.Error("fx 启动回滚", "err", ev.Err)
	}
}

// healthProbeTimeout 单轮健康探测的整体预算。
const healthProbeTimeout = 30 * time.Second

// provideLogger 提供进程级结构化日志,并设为 slog 默认。
// @return 结构化日志
func provideLogger() *slog.Logger {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	return logger
}

// provideConfig 加载运行配置,并确保数据目录存在(后续 DB 与密钥文件都落其下)。
// @return 运行配置
// @return 加载失败或数据目录不可创建时的错误
func provideConfig() (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return config.Config{}, fmt.Errorf("创建数据目录 %q: %w", cfg.DataDir, err)
	}
	return cfg, nil
}

// provideDB 打开并迁移元数据库,关闭挂到 fx 停机钩子。
// @return 数据库连接
// @return 打开或迁移失败时的错误
func provideDB(lc fx.Lifecycle, cfg config.Config, logger *slog.Logger) (*sql.DB, error) {
	conn, err := db.Open(filepath.Join(cfg.DataDir, "imgyoung.db"))
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		if cerr := conn.Close(); cerr != nil {
			logger.Error("关闭数据库失败", "err", cerr)
			return cerr
		}
		return nil
	}})
	return conn, nil
}

// provideQueries 提供 sqlc 查询句柄。
// @return 查询句柄
func provideQueries(conn *sql.DB) *db.Queries { return db.New(conn) }

// provideMasterKey 解析主密钥:配置(hex)优先,否则数据目录文件(首启生成)。
// @return 主密钥
// @return 密钥非法或文件读写失败时的错误
func provideMasterKey(cfg config.Config, logger *slog.Logger) (*secret.MasterKey, error) {
	if cfg.MasterKey != "" {
		key, err := secret.MasterKeyFromHex(cfg.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("master_key 配置非法: %w", err)
		}
		return key, nil
	}
	key, created, err := secret.LoadOrCreate(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if created {
		logger.Warn("已生成主密钥文件,请立即备份——丢失将导致渠道配置与加密渠道数据永久不可恢复",
			"path", filepath.Join(cfg.DataDir, "master.key"))
	}
	return key, nil
}

// provideToken 提供上传鉴权 Token;必填,未配置即拒绝启动(fail-fast)。
// @Description: 鉴权凭证不做"自动生成"兜底——每次重启都变的临时 Token 是运维陷阱,
// 且与本项目快速失败红线一致。多用户具名 Token(哈希入库)见迭代 5。
// @return 上传鉴权 Token
// @return 未配置 Token 时的错误
func provideToken(cfg config.Config) (string, error) {
	if cfg.Token == "" {
		return "", errors.New("未配置 IMGYOUNG_TOKEN:上传鉴权 Token 必填,请设置后重启")
	}
	return cfg.Token, nil
}

// provideManager 构造渠道管理器(首次加载用后台 context)。
// @return 渠道管理器
// @return 加载或驱动构造失败时的错误
func provideManager(cfg config.Config, conn *sql.DB, queries *db.Queries,
	master *secret.MasterKey, logger *slog.Logger) (*channel.Manager, error) {
	return channel.NewManager(context.Background(), channel.ManagerConfig{
		Conn:    conn,
		Queries: queries,
		Master:  master,
		DataDir: cfg.DataDir,
		Logger:  logger,
	})
}

// provideRunner 构造补齐子系统,River 客户端的启停挂到 fx 生命周期。
// @Description: Runner 用独立于信号的长生命周期 context——停机由 OnStop 优雅收敛,
// 而非信号直接砍断在途复制。
// @return 补齐 Runner
// @return River 初始化失败时的错误
func provideRunner(lc fx.Lifecycle, conn *sql.DB, queries *db.Queries,
	manager *channel.Manager, logger *slog.Logger) (*replication.Runner, error) {
	runner, err := replication.NewRunner(replication.RunnerConfig{
		Conn:    conn,
		Queries: queries,
		Manager: manager,
		Logger:  logger,
	})
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error { return runner.Start(runCtx) },
		OnStop: func(ctx context.Context) error {
			err := runner.Stop(ctx)
			cancel()
			return err
		},
	})
	return runner, nil
}

// provideUpload 构造上传服务。
// @return 上传服务
func provideUpload(cfg config.Config, conn *sql.DB, queries *db.Queries,
	manager *channel.Manager, runner *replication.Runner, logger *slog.Logger) *upload.Service {
	return upload.New(upload.Config{
		Conn:       conn,
		Queries:    queries,
		Channels:   manager,
		Replicator: runner,
		MaxPixels:  cfg.MaxPixels,
		MinSuccess: cfg.MinSuccess,
		Logger:     logger,
	})
}

// provideMetrics 构造指标集,并挂上读取渠道健康的量表(闭包依赖 core,留在装配根)。
// @return 指标集
func provideMetrics(manager *channel.Manager) *observability.Metrics {
	m := observability.New()
	m.RegisterGauge("channels_healthy_total", "当前健康渠道数。",
		func() float64 { return float64(len(manager.Healthy())) })
	m.RegisterGauge("channels_total", "已加载渠道总数(含不健康)。",
		func() float64 { return float64(len(manager.All())) })
	return m
}

// provideAudit 构造审计记录器。
// @return 审计记录器
func provideAudit(queries *db.Queries) *audit.Recorder { return audit.New(queries) }

// provideServer 构造 HTTP 业务处理器集合。
// @return HTTP 服务处理器
func provideServer(cfg config.Config, queries *db.Queries, manager *channel.Manager,
	uploadSvc *upload.Service, token string, metrics *observability.Metrics,
	auditRec *audit.Recorder, tracker *lifecycle.Tracker, logger *slog.Logger) *server.Server {
	return server.New(server.Config{
		Queries:              queries,
		Channels:             manager,
		Admin:                manager,
		EncryptNewChannels:   cfg.EncryptNewChannels,
		RedirectMode:         cfg.AccessMode == "redirect",
		Upload:               uploadSvc,
		Token:                token,
		MaxUploadBytes:       cfg.MaxUploadBytes,
		MaxConcurrentUploads: cfg.MaxConcurrentUploads,
		BaseURL:              cfg.BaseURL,
		DefaultEXIFPolicy:    cfg.EXIFPolicy,
		RefererWhitelist:     cfg.RefererWhitelist,
		AllowEmptyReferer:    cfg.AllowEmptyReferer,
		DefaultTTL:           time.Duration(cfg.DefaultTTLHours) * time.Hour,
		Activity:             tracker,
		Metrics:              metrics,
		Audit:                auditRec,
		Logger:               logger,
	})
}

// provideSweeper 构造生命周期清理器(渠道对象删除走 replication 入队口)。
// @return 清理器
func provideSweeper(cfg config.Config, conn *sql.DB, queries *db.Queries,
	runner *replication.Runner, logger *slog.Logger) *lifecycle.Sweeper {
	return lifecycle.New(lifecycle.Config{
		Conn:          conn,
		Queries:       queries,
		Deleter:       runner,
		Grace:         time.Duration(cfg.DeleteGraceMinutes) * time.Minute,
		ColdThreshold: time.Duration(cfg.ColdCleanupDays) * 24 * time.Hour,
		Logger:        logger,
	})
}

// provideTracker 构造访问活跃度追踪器。
// @return 活跃度追踪器
func provideTracker(conn *sql.DB, queries *db.Queries, logger *slog.Logger) *lifecycle.Tracker {
	return lifecycle.NewTracker(conn, queries, logger)
}

// bytesPerGB 一 GB 的字节数(渠道配额阈值换算)。
const bytesPerGB = 1 << 30

// provideMerger 构造精确查重合并器(冗余 blob 的物理删宽限与生命周期一致)。
// @return 查重合并器
func provideMerger(cfg config.Config, conn *sql.DB, queries *db.Queries, logger *slog.Logger) *dedup.Merger {
	return dedup.New(dedup.Config{
		Conn:    conn,
		Queries: queries,
		Grace:   time.Duration(cfg.DeleteGraceMinutes) * time.Minute,
		Logger:  logger,
	})
}

// provideUsageReporter 构造渠道用量上报器(用量量表走 observability)。
// @return 用量上报器
func provideUsageReporter(cfg config.Config, queries *db.Queries,
	metrics *observability.Metrics, logger *slog.Logger) *usage.Reporter {
	return usage.New(usage.Config{
		Queries:    queries,
		Gauge:      metrics,
		QuotaBytes: int64(cfg.ChannelQuotaGB) * bytesPerGB,
		Logger:     logger,
	})
}

// provideHTTPServer 组装 net/http.Server,监听与优雅停机挂到 fx 生命周期。
// @Description: OnStart 先建监听(绑定失败即启动失败),再后台 Serve;OnStop 走 Shutdown。
// @return HTTP 服务器
func provideHTTPServer(lc fx.Lifecycle, cfg config.Config, srv *server.Server, logger *slog.Logger) *http.Server {
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ln, err := net.Listen("tcp", httpSrv.Addr)
			if err != nil {
				return fmt.Errorf("监听 %q: %w", httpSrv.Addr, err)
			}
			go func() {
				if serr := httpSrv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
					logger.Error("HTTP 服务异常退出", "err", serr)
				}
			}()
			logger.Info("ImgYoung 已启动", "listen", cfg.Listen, "data_dir", cfg.DataDir)
			return nil
		},
		OnStop: func(ctx context.Context) error { return httpSrv.Shutdown(ctx) },
	})
	return httpSrv
}

// registerBackgroundLoops 注册渠道健康检查与元数据库备份两个周期循环。
// @Description: 各自持长生命周期 context,OnStop 取消。二者均为兜底型循环,启动即先跑一轮。
func registerBackgroundLoops(lc fx.Lifecycle, cfg config.Config, conn *sql.DB,
	manager *channel.Manager, sweeper *lifecycle.Sweeper, tracker *lifecycle.Tracker,
	reporter *usage.Reporter, merger *dedup.Merger, logger *slog.Logger) {
	usageCtx, usageCancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go reporter.Run(usageCtx, time.Duration(cfg.UsageReportSeconds)*time.Second)
			return nil
		},
		OnStop: func(context.Context) error { usageCancel(); return nil },
	})

	dedupCtx, dedupCancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go merger.Run(dedupCtx, time.Duration(cfg.DedupScanSeconds)*time.Second)
			return nil
		},
		OnStop: func(context.Context) error { dedupCancel(); return nil },
	})

	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go sweeper.Run(sweepCtx, time.Duration(cfg.CleanupSweepSeconds)*time.Second)
			return nil
		},
		OnStop: func(context.Context) error { sweepCancel(); return nil },
	})

	activityCtx, activityCancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go tracker.Run(activityCtx, time.Duration(cfg.ActivityFlushSeconds)*time.Second)
			return nil
		},
		OnStop: func(context.Context) error { activityCancel(); return nil },
	})

	healthCtx, healthCancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error { go healthLoop(healthCtx, cfg, manager, logger); return nil },
		OnStop:  func(context.Context) error { healthCancel(); return nil },
	})

	backupSvc := reliability.New(reliability.Config{
		Conn: conn, Channels: manager, DataDir: cfg.DataDir, Keep: cfg.BackupKeep, Logger: logger,
	})
	backupCtx, backupCancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go backupSvc.Run(backupCtx, time.Duration(cfg.BackupIntervalHours)*time.Hour)
			return nil
		},
		OnStop: func(context.Context) error { backupCancel(); return nil },
	})
}

// healthLoop 周期探测各渠道健康,状态变化写回 DB 并更新内存标记。
// @param ctx 控制循环生命周期
func healthLoop(ctx context.Context, cfg config.Config, manager *channel.Manager, logger *slog.Logger) {
	ticker := time.NewTicker(time.Duration(cfg.HealthCheckSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
			changed, err := manager.CheckAll(probeCtx)
			cancel()
			if err != nil {
				logger.Warn("健康检查未完成", "err", err)
			}
			if len(changed) > 0 {
				logger.Info("渠道健康状态变化", "channels", changed)
			}
		}
	}
}
