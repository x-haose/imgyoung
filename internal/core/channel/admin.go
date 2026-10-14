package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/store/driver"
	"github.com/x-haose/imgyoung/internal/store/rclone"
)

// ErrInvalidChannel 渠道创建参数非法(名称/后端为空或后端无法初始化)。
var ErrInvalidChannel = errors.New("channel: 渠道参数非法")

// ErrChannelNameExists 渠道名已存在(唯一约束冲突);由 Create/Update 收敛底层
// DB 错误得来,调用方以 errors.Is 判定,不必自行匹配驱动错误文本。
var ErrChannelNameExists = errors.New("channel: 渠道名已存在")

// ErrChannelNotFound 目标渠道不存在。
var ErrChannelNotFound = errors.New("channel: 渠道不存在")

// ErrEncryptedImmutable 加密属性创建后不可变:翻转会造成同一渠道密文/明文对象
// 混存,历史数据不可恢复,只能新建渠道迁移。
var ErrEncryptedImmutable = errors.New("channel: 加密属性创建后不可变")

// CreateParams 创建渠道的参数。
type CreateParams struct {
	Name             string            // 渠道名(唯一)
	Driver           string            // rclone 后端名
	Config           map[string]string // 后端参数(含约定键 path / public_base_url),可为 nil;密码类值明文或 rclone 编码产物皆可
	Template         string            // 配置所用模板 id,空 = 自定义;仅供编辑表单还原,不参与驱动构造
	Priority         int64             // 优先级,越大越优先
	Encrypted        bool              // 是否为加密渠道;创建时固化,建后不可变
	AllowDirectLink  bool              // 是否允许 302 直链
	AutoCreateBucket bool              // 是否在创建时主动建桶(需凭证有建桶权限);仅 s3 有意义
}

// nameMaxLen 渠道名长度上限。
const nameMaxLen = 64

// configKeyPattern 渠道配置键的合法字符集(与 rclone 参数名一致)。
var configKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// validateCreateParams 校验创建参数:必填项、渠道名长度、配置键字符集、
// 加密与直链的互斥(密文无法直出)。
// @param p 创建参数
// @return 参数非法时的 ErrInvalidChannel
func validateCreateParams(p CreateParams) error {
	if p.Name == "" || p.Driver == "" {
		return fmt.Errorf("%w: name 与 driver 必填", ErrInvalidChannel)
	}
	if len(p.Name) > nameMaxLen {
		return fmt.Errorf("%w: name 超过 %d 字节", ErrInvalidChannel, nameMaxLen)
	}
	if p.Encrypted && p.AllowDirectLink {
		return fmt.Errorf("%w: 加密渠道不允许直链(密文无法直出)", ErrInvalidChannel)
	}
	for k := range p.Config {
		if k == configKeyPath || k == configKeyPublicBaseURL {
			continue
		}
		if !configKeyPattern.MatchString(k) {
			return fmt.Errorf("%w: 配置键 %q 非法(仅允许字母数字下划线)", ErrInvalidChannel, k)
		}
	}
	return nil
}

// prepareConfig 把提交的配置整理成可入库形态:复制一份(不改入参),密码类值按 rclone 要求编码。
// @param driverName rclone 后端名
// @param config 提交的配置,可为 nil
// @return 整理后的新配置
// @return 后端不存在时的 ErrInvalidChannel
func prepareConfig(driverName string, config map[string]string) (map[string]string, error) {
	out := maps.Clone(config)
	if out == nil {
		out = map[string]string{}
	}
	if err := rclone.ObscurePlainSecrets(driverName, out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	return out, nil
}

// Info 渠道的对外描述(不含任何配置与凭证)。
type Info struct {
	ID              int64        `json:"id"`                // 渠道主键
	Name            string       `json:"name"`              // 渠道名
	Driver          string       `json:"driver"`            // 后端名
	Priority        int64        `json:"priority"`          // 优先级
	Healthy         bool         `json:"healthy"`           // 健康状态
	Encrypted       bool         `json:"encrypted"`         // 是否加密渠道
	AllowDirectLink bool         `json:"allow_direct_link"` // 是否允许直链
	LastCheckedAt   string       `json:"last_checked_at"`   // 最近探测时刻,UTC 文本;空 = 尚未探测
	LastError       string       `json:"last_error"`        // 最近探测失败原因(人话化);空 = 最近一次探测成功
	Deleting        bool         `json:"deleting"`          // 清理中:排空任务正在删除渠道上的对象,清完即消失
	Replicas        ReplicaStats `json:"replicas"`          // 副本补齐水位统计
}

// ReplicaStats 渠道的副本补齐水位(按 blob_channels 状态聚合)。
type ReplicaStats struct {
	Done    int64 `json:"done"`    // 已就绪副本数
	Pending int64 `json:"pending"` // 待补齐副本数(含补齐进行中)
	Failed  int64 `json:"failed"`  // 补齐失败副本数
}

// boolToInt 把布尔转为 SQLite 存储的 0/1。
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Create 创建渠道:试构造验证配置 → 落库并播种存量副本 → 重载 → 立即深探测。
// @Description: 深探测失败不回滚创建——渠道保留,healthy=false 且 last_error
// 可见,管理员据此修配置或重检。
// @param p 创建参数
// @return 新渠道 ID
// @return 参数非法、后端初始化失败或落库失败时的错误
func (m *Manager) Create(ctx context.Context, p CreateParams) (int64, error) {
	if err := validateCreateParams(p); err != nil {
		return 0, err
	}
	config, err := prepareConfig(p.Driver, p.Config)
	if err != nil {
		return 0, err
	}
	// 试构造:落库前验证后端名与参数能初始化(crypt 派生依赖渠道 ID,
	// 加密渠道以明文形态试构造,包裹层在 Reload 时再生效)。
	trial, err := m.openTrial(ctx, p.Driver, config)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	m.closeTrial(ctx, trial, "创建试构造")

	// 可选自动建桶:在落库前主动建,失败即中止(不留"渠道已建但桶不存在"的坑)。
	if p.AutoCreateBucket {
		if err := m.ensureBucket(ctx, p.Driver, config); err != nil {
			return 0, fmt.Errorf("%w: 自动创建存储桶失败(请确认密钥具备建桶权限,或先在控制台建好桶并关闭此项): %w", ErrInvalidChannel, err)
		}
	}
	sealed, err := m.sealConfigMap(config)
	if err != nil {
		return 0, err
	}
	channelID, seeded, err := m.insertChannelTx(ctx, p, sealed)
	if err != nil {
		return 0, err
	}
	if seeded > 0 {
		m.cfg.Logger.Info("已为新渠道播种存量副本", "channel", p.Name, "count", seeded)
	}
	// 渠道已落库;装载失败只告警(客户端已获 ID,重启或下次 Reload 可恢复)。
	if err := m.Reload(ctx); err != nil {
		m.cfg.Logger.Error("渠道已创建但装载失败", "channel", p.Name, "err", err)
		return channelID, nil
	}
	if err := m.ProbeChannel(ctx, channelID); err != nil {
		m.cfg.Logger.Warn("新渠道首次探测未完成", "channel", p.Name, "err", err)
	}
	return channelID, nil
}

// insertChannelTx 在单个事务里落渠道记录并为存量 blob 播种 pending 副本行。
// @Description: 落库与播种同事务——播种时刻已入库的 blob 立即获得 pending 行
// (pending 行是补齐管道的唯一真相);与在途上传并发时,上传按其开始时的渠道
// 快照落行、看不见本渠道,这类缺行由周期对账的收敛播种补齐。
// @param p 创建参数
// @param sealed 已加密的配置密文
// @return 新渠道 ID 与播种的副本行数
// @return ErrChannelNameExists 或落库失败时的错误
func (m *Manager) insertChannelTx(ctx context.Context, p CreateParams, sealed string) (int64, int64, error) {
	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := m.cfg.Queries.WithTx(tx)
	channelID, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name:            p.Name,
		Driver:          p.Driver,
		ConfigSealed:    sealed,
		Priority:        p.Priority,
		Encrypted:       boolToInt(p.Encrypted),
		AllowDirectLink: boolToInt(p.AllowDirectLink),
		Template:        p.Template,
	})
	if err != nil {
		// SQLite 驱动不暴露类型化的约束错误,按消息收敛为哨兵——匹配集中此一处,不外泄到 HTTP 层。
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return 0, 0, ErrChannelNameExists
		}
		return 0, 0, fmt.Errorf("写入渠道记录: %w", err)
	}
	seeded, err := q.SeedBlobChannelsForChannel(ctx, channelID)
	if err != nil {
		return 0, 0, fmt.Errorf("播种存量副本: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("提交事务: %w", err)
	}
	return channelID, seeded, nil
}

// UpdateParams 编辑渠道的参数;配置整体替换,不做增量合并。
type UpdateParams struct {
	Name            string            // 渠道名(唯一)
	Driver          string            // rclone 后端名
	Config          map[string]string // 后端参数(含约定键 path / public_base_url),整体替换;值为 SecretPlaceholder 表示保留原值
	Template        string            // 配置所用模板 id,空 = 自定义;仅供编辑表单还原,不参与驱动构造
	Priority        int64             // 优先级,越大越优先
	Encrypted       *bool             // 期望的加密属性;nil 表示不动,与现状不一致时拒绝
	AllowDirectLink bool              // 是否允许 302 直链
}

// Update 编辑渠道:校验后整体替换配置,重建驱动实例并立即深探测。
// @Description: encrypted 不可变(翻转会造成密文/明文混存)。配置中的
// SecretPlaceholder 值先还原为该渠道已存配置的同键原值(编辑表单不必重录凭证)。
// 成功后 Reload 重建本渠道驱动,再对该渠道深探测落库。编辑不改变副本集合语义,不触发存量回填。
// @param channelID 渠道主键
// @param p 编辑参数
// @return ErrChannelNotFound / ErrEncryptedImmutable / ErrChannelNameExists /
// ErrInvalidChannel 或落库、重载失败时的错误
func (m *Manager) Update(ctx context.Context, channelID int64, p UpdateParams) error {
	if err := m.updateRecord(ctx, channelID, p); err != nil {
		return err
	}
	if err := m.Reload(ctx); err != nil {
		return fmt.Errorf("渠道已更新但重载失败: %w", err)
	}
	if err := m.ProbeChannel(ctx, channelID); err != nil {
		m.cfg.Logger.Warn("编辑后探测未完成", "channel", p.Name, "err", err)
	}
	return nil
}

// updateRecord 校验编辑参数并写回渠道记录;配置密文的读改写全程持锁,与后端写回互斥。
// @param channelID 渠道主键
// @param p 编辑参数
// @return ErrChannelNotFound / ErrEncryptedImmutable / ErrChannelNameExists /
// ErrInvalidChannel 或落库失败时的错误
func (m *Manager) updateRecord(ctx context.Context, channelID int64, p UpdateParams) error {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("读取渠道 %d: %w", channelID, err)
	}
	encrypted := row.Encrypted != 0
	if p.Encrypted != nil && *p.Encrypted != encrypted {
		return ErrEncryptedImmutable
	}
	old, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		return err
	}
	restored, err := restoreSecrets(p.Config, old)
	if err != nil {
		return err
	}
	if err := validateCreateParams(CreateParams{
		Name: p.Name, Driver: p.Driver, Config: restored,
		Encrypted: encrypted, AllowDirectLink: p.AllowDirectLink,
	}); err != nil {
		return err
	}
	config, err := prepareConfig(p.Driver, restored)
	if err != nil {
		return err
	}
	// 试构造:与 Create 同规则,加密渠道以明文形态验证后端可初始化。
	trial, err := m.openTrial(ctx, p.Driver, config)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	m.closeTrial(ctx, trial, "编辑试构造")
	sealed, err := m.sealConfigMap(config)
	if err != nil {
		return err
	}
	if err := m.cfg.Queries.UpdateChannelConfig(ctx, sqlc.UpdateChannelConfigParams{
		Name: p.Name, Driver: p.Driver, ConfigSealed: sealed, Priority: p.Priority,
		AllowDirectLink: boolToInt(p.AllowDirectLink), Template: p.Template, ID: channelID,
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrChannelNameExists
		}
		return fmt.Errorf("更新渠道记录: %w", err)
	}
	return nil
}

// ensureBucket 对 s3 类渠道主动建桶。
// @Description: 用渠道配置构造一个显式 no_check_bucket=false 的裸驱动(不套 crypt——
// 建桶是桶级操作,只需内层 s3),再调 EnsureBucket。渠道自身仍用默认 no_check_bucket=true,
// 建好一次后续不必每次探测。
// @param drv rclone 后端名
// @param config 已整理的渠道配置(含约定键 path)
// @return 建桶失败时的错误
func (m *Manager) ensureBucket(ctx context.Context, drv string, config map[string]string) error {
	params := maps.Clone(config)
	params["no_check_bucket"] = "false"
	d, err := m.openTrial(ctx, drv, params)
	if err != nil {
		return fmt.Errorf("初始化建桶驱动: %w", err)
	}
	defer m.closeTrial(ctx, d, "建桶")
	return d.EnsureBucket(ctx)
}

// Delete 删除渠道;若其持有任何图的唯一就绪副本则拒绝(防数据丢失)。
// @Description: 唯一副本检查与删除在同一事务内完成,消除并发上传落库造成的检查-删除窗口;
// 其他清理中的渠道不算可替代副本,两个渠道先后带清理删除不会把同一张图的副本删光。
// purge 为 true 只打清理中标记:渠道即刻退出选路、补齐与探测,渠道上的对象由排空任务
// 逐个删除、清完才删行(见 Remove);为 false 立即删行,渠道上的对象原样留在远端。
// @param channelID 渠道主键
// @param purge 是否清理渠道上的对象
// @return ErrHoldsSoleReplicas 或落库、重载失败时的错误
func (m *Manager) Delete(ctx context.Context, channelID int64, purge bool) error {
	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := m.cfg.Queries.WithTx(tx)
	sole, err := q.CountSoleReplicas(ctx, channelID)
	if err != nil {
		return fmt.Errorf("检查唯一副本: %w", err)
	}
	if sole > 0 {
		return fmt.Errorf("%w(%d 张图仅此渠道有副本)", ErrHoldsSoleReplicas, sole)
	}
	if purge {
		if err := q.MarkChannelPendingDelete(ctx, channelID); err != nil {
			return fmt.Errorf("标记渠道清理中: %w", err)
		}
	} else if err := removeChannelRows(ctx, q, channelID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return m.Reload(ctx)
}

// Remove 删除渠道记录及其全部副本记录,不检查唯一副本、不触碰远端对象。
// @Description: 供排空任务清完对象后收尾;唯一副本检查在打清理中标记时已经做过,
// 之后该渠道的副本不再被任何路径计入。
// @param channelID 渠道主键
// @return 落库或重载失败时的错误
func (m *Manager) Remove(ctx context.Context, channelID int64) error {
	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := removeChannelRows(ctx, m.cfg.Queries.WithTx(tx), channelID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return m.Reload(ctx)
}

// removeChannelRows 在事务内删除渠道的副本记录与渠道记录(副本记录有外键指向渠道,先删)。
// @param q 绑定事务的查询集
// @param channelID 渠道主键
// @return 删除失败时的错误
func removeChannelRows(ctx context.Context, q *sqlc.Queries, channelID int64) error {
	if err := q.DeleteBlobChannelsByChannel(ctx, channelID); err != nil {
		return fmt.Errorf("清理渠道副本记录: %w", err)
	}
	if err := q.DeleteChannel(ctx, channelID); err != nil {
		return fmt.Errorf("删除渠道记录: %w", err)
	}
	return nil
}

// List 返回全部渠道的对外描述(按优先级降序,不含配置与凭证)。
// @Description: 副本水位来自 blob_channels 的一次聚合查询,渠道数量无关(非 N+1);
// 无任何副本行的渠道计数为零值。
// @return 渠道描述列表
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	rows, err := m.cfg.Queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询渠道列表: %w", err)
	}
	stats, err := m.cfg.Queries.ChannelReplicaStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计渠道副本水位: %w", err)
	}
	statByID := make(map[int64]sqlc.ChannelReplicaStatsRow, len(stats))
	for _, s := range stats {
		statByID[s.ChannelID] = s
	}
	out := make([]Info, 0, len(rows))
	for _, row := range rows {
		s := statByID[row.ID]
		out = append(out, Info{
			ID:              row.ID,
			Name:            row.Name,
			Driver:          row.Driver,
			Priority:        row.Priority,
			Healthy:         row.Healthy != 0,
			Encrypted:       row.Encrypted != 0,
			AllowDirectLink: row.AllowDirectLink != 0,
			LastCheckedAt:   row.LastCheckedAt.String,
			LastError:       row.LastError.String,
			Deleting:        row.PendingDelete != 0,
			Replicas:        ReplicaStats{Done: s.Done, Pending: s.Pending, Failed: s.Failed},
		})
	}
	return out, nil
}

// probeTimeout 单渠道轻探测超时;各渠道独立计时,慢渠道不拖累其他渠道。
const probeTimeout = 10 * time.Second

// HealthProbeKey 健康探测使用的约定对象键(从不真正写入;对账 GC 应豁免)。
const HealthProbeKey = ".imgyoung-health-probe"

// CheckAll 对全部渠道并行做一次健康探测,结果逐渠道落库并更新内存标记。
// @Description: 轻探测 = 只读 Stat 一个约定不存在的对象:ErrNotFound 与成功都视为
// 健康(后端可达),其余错误视为不健康。轻探测只能降级健康渠道,不能独自恢复不健康
// 渠道——失败结论可能来自暴露写路径问题的深探测(如只读凭证),只读成功不构成推翻依据,
// 翻回健康须经深探测确认写读删全通;健康渠道每轮维持零写删成本(周期性写删对按量
// 付费渠道是无谓账单)。各渠道并行、各自计时,整轮耗时等于最慢的一个;ctx 取消
// (停机)时整轮作废不落库——取消造成的探测失败不是渠道的结论。
// 健康翻转只改标记、不重建驱动;每渠道每轮都更新 last_checked_at,失败原因人话化
// 后存 last_error、成功清空。
// @return 本次健康状态翻转的渠道名列表
// @return ctx 已取消或落库失败时的错误
func (m *Manager) CheckAll(ctx context.Context) ([]string, error) {
	channels := m.snapshot()
	results := make([]error, len(channels))
	var wg sync.WaitGroup
	for i, c := range channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = probeForHealth(ctx, c.handle.Driver, c.healthy)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	changed := []string{}
	for i, c := range channels {
		if err := m.recordProbe(ctx, c.handle.ID, results[i]); err != nil {
			return changed, fmt.Errorf("渠道 %q: %w", c.handle.Name, err)
		}
		if c.healthy != (results[i] == nil) {
			changed = append(changed, c.handle.Name)
		}
	}
	return changed, nil
}

// snapshot 返回当前在役渠道的副本(句柄与健康标记),供一轮探测使用;清理中的渠道不探测。
// @return 渠道运行态切片
func (m *Manager) snapshot() []managed {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]managed, 0, len(m.channels))
	for _, c := range m.channels {
		if !c.handle.PendingDelete {
			out = append(out, c)
		}
	}
	return out
}

// probeForHealth 对单个渠道做一轮健康判定:轻探测;不健康渠道轻探测通过后再经深探测确认才算恢复。
// @param d 渠道驱动
// @param healthy 渠道当前的健康标记
// @return 渠道不可用时的原因错误;nil 表示健康
func probeForHealth(ctx context.Context, d driver.Driver, healthy bool) error {
	if err := probe(ctx, d); err != nil || healthy {
		return err
	}
	return DeepProbe(ctx, d)
}

// probe 轻量探测单个渠道可达性(独立超时,只读不写)。
// @param d 渠道驱动
// @return 渠道不可达时的原因错误;nil 表示健康
func probe(ctx context.Context, d driver.Driver) error {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := d.Stat(probeCtx, HealthProbeKey)
	if err == nil || errors.Is(err, driver.ErrNotFound) {
		return nil
	}
	return err
}
