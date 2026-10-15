package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"
	"github.com/x-haose/imgyoung/store/rclone"
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
	Template         string            // 配置所用模板 id,空 = 自定义;模板表单的密码类值按明文编码,并供编辑表单还原
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

// prepareConfig 把提交的配置整理成可入库形态:哨兵值还原为已存原值,本次提交的密码类值按 rclone 要求编码。
// @Description: 哨兵只在回读时掩码过的敏感键上有效。还原出的值取自已入库配置,本就是编码产物,
// 不再编码;只编码本次提交的值——模板表单的值确定是明文,一律编码;自定义参数来源不明,
// 已是编码产物的值原样保留(见 rclone.ObscurePlainSecrets)。
// @param driverName rclone 后端名
// @param submitted 提交的配置,可为 nil;不修改
// @param stored 渠道已存的配置;创建场景为 nil
// @param fromTemplate 配置是否由模板表单合并而来
// @return 整理后的新配置
// @return 哨兵落在非敏感键或无原值可还原、后端不存在时的 ErrInvalidChannel
func prepareConfig(driverName string, submitted, stored map[string]string,
	fromTemplate bool) (map[string]string, error) {
	secret, err := secretKeysOf(driverName)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(submitted))
	kept := make(map[string]string)
	for k, v := range submitted {
		switch {
		case v != SecretPlaceholder:
			out[k] = v
		case !secret[k]:
			return nil, fmt.Errorf("%w: 字段 %q 不是凭证,不能沿用已存值", ErrInvalidChannel, k)
		case stored[k] == "":
			return nil, fmt.Errorf("%w: 字段 %q 无已存原值可保留,请重新填写", ErrInvalidChannel, k)
		default:
			kept[k] = stored[k]
		}
	}
	encode := rclone.ObscurePlainSecrets
	if fromTemplate {
		encode = rclone.ObscureSecrets
	}
	if err := encode(driverName, out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	maps.Copy(out, kept)
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
	CreatedAt       string       `json:"created_at"`        // 创建时刻,UTC 文本;与 ID 一起参与加密渠道的 crypt 密钥派生
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

// Create 创建渠道:整理配置 → 试构造验证 → 落库并播种存量副本 → 重载 → 立即深探测。
// @Description: 整理配置含编码密码与补齐授权带出的参数(见 completeTemplateConfig)。深探测失败
// 不回滚创建——渠道保留,healthy=false 且 last_error 可见,管理员据此修配置或重检。
// @param p 创建参数
// @return 新渠道 ID
// @return 参数非法、后端初始化失败或落库失败时的错误
func (m *Manager) Create(ctx context.Context, p CreateParams) (int64, error) {
	if err := validateCreateParams(p); err != nil {
		return 0, err
	}
	config, err := prepareConfig(p.Driver, p.Config, nil, p.Template != "")
	if err != nil {
		return 0, err
	}
	if err := completeTemplateConfig(ctx, p.Template, p.Config, config, nil); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	if err := m.verifyConfig(ctx, p.Driver, config, p.AutoCreateBucket); err != nil {
		return 0, err
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
	// 渠道已落库,创建即成立;装不上的渠道原因已落库,健康检查时重试装载。
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
// @return channelID 新渠道 ID
// @return seeded 播种的副本行数
// @return err ErrChannelNameExists 或落库失败时的错误
func (m *Manager) insertChannelTx(ctx context.Context, p CreateParams,
	sealed string) (channelID, seeded int64, err error) {
	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := m.cfg.Queries.WithTx(tx)
	channelID, err = q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name:            p.Name,
		Driver:          p.Driver,
		ConfigSealed:    sealed,
		Priority:        p.Priority,
		Encrypted:       boolToInt(p.Encrypted),
		AllowDirectLink: boolToInt(p.AllowDirectLink),
		Template:        p.Template,
	})
	if err != nil {
		if db.IsUniqueViolation(err, "channels.name") {
			return 0, 0, ErrChannelNameExists
		}
		return 0, 0, fmt.Errorf("写入渠道记录: %w", err)
	}
	seeded, err = q.SeedBlobChannelsForChannel(ctx, channelID)
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
	Template        string            // 配置所用模板 id,空 = 自定义;模板表单的密码类值按明文编码,并供编辑表单还原
	Priority        int64             // 优先级,越大越优先
	Encrypted       *bool             // 期望的加密属性;nil 表示不动,与现状不一致时拒绝
	AllowDirectLink bool              // 是否允许 302 直链
}

// Update 编辑渠道:校验后整体替换配置,重建驱动实例并立即深探测。
// @Description: encrypted 不可变(翻转会造成密文/明文混存)。配置中的
// SecretPlaceholder 值先还原为该渠道已存配置的同键原值(编辑表单不必重录凭证),
// 沿用已存授权时授权带出的参数一并保留。后端名或配置有任何改动即把绑定时刻重置为当前时刻。
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
// @Description: 后端名或落库配置(哨兵还原后的参数)与原有的有任何不同,即把绑定时刻重置为当前时刻:
// 决定存储位置的参数因后端而异(如 OneDrive 的 drive_id 属敏感参数),一律按改了位置处理,保证对账 GC
// 不回收新位置上原有的对象。比较在试构造之前,试构造期间令牌刷新的写回不计入。
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
	old, config, err := m.editedConfig(ctx, row, p)
	if err != nil {
		return err
	}
	rebind := p.Driver != row.Driver || !maps.Equal(old, config)
	if err := m.verifyConfig(ctx, p.Driver, config, false); err != nil {
		return err
	}
	sealed, err := m.sealConfigMap(config)
	if err != nil {
		return err
	}
	boundAt := row.BoundAt
	if rebind {
		boundAt = sqlc.TimeText(time.Now())
	}
	if err := m.cfg.Queries.UpdateChannelConfig(ctx, sqlc.UpdateChannelConfigParams{
		Name: p.Name, Driver: p.Driver, ConfigSealed: sealed, Priority: p.Priority,
		AllowDirectLink: boolToInt(p.AllowDirectLink), Template: p.Template, BoundAt: boundAt, ID: channelID,
	}); err != nil {
		if db.IsUniqueViolation(err, "channels.name") {
			return ErrChannelNameExists
		}
		return fmt.Errorf("更新渠道记录: %w", err)
	}
	return nil
}

// editedConfig 校验编辑参数,并把提交的配置整理成将要落库的形态(哨兵还原、密码编码、补齐授权带出的参数)。
// @param row 渠道现有记录
// @param p 编辑参数
// @return old 渠道已存的配置
// @return config 将要落库的配置
// @return ErrEncryptedImmutable / ErrInvalidChannel 或解密已存配置失败时的错误
func (m *Manager) editedConfig(ctx context.Context, row sqlc.Channel, p UpdateParams) (old, config map[string]string,
	err error) {
	encrypted := row.Encrypted != 0
	if p.Encrypted != nil && *p.Encrypted != encrypted {
		return nil, nil, ErrEncryptedImmutable
	}
	if err := validateCreateParams(CreateParams{
		Name: p.Name, Driver: p.Driver, Config: p.Config,
		Encrypted: encrypted, AllowDirectLink: p.AllowDirectLink,
	}); err != nil {
		return nil, nil, err
	}
	old, err = m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		return nil, nil, err
	}
	config, err = prepareConfig(p.Driver, p.Config, old, p.Template != "")
	if err != nil {
		return nil, nil, err
	}
	if err := completeTemplateConfig(ctx, p.Template, p.Config, config, old); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	return old, config, nil
}

// verifyConfig 落库前以一次性驱动试构造整理好的配置,可选顺带建桶;试构造期间的写回并入配置(原地修改)。
// @Description: 加密渠道以明文形态试构造——crypt 密钥派生依赖渠道 ID 与创建时刻,包裹层在 Reload 时
// 生效。写回多为访问令牌过期时刷新得到的新令牌,随配置落库,刷新令牌只能用一次的服务商不因此作废。
// 建桶在落库前进行,失败即中止,不留「渠道已建但桶不存在」的坑。
// @param driverName rclone 后端名
// @param config 已整理的配置
// @param createBucket 是否主动建桶(需凭证有建桶权限);仅 s3 有意义
// @return 后端初始化或建桶失败时的 ErrInvalidChannel
func (m *Manager) verifyConfig(ctx context.Context, driverName string, config map[string]string,
	createBucket bool) error {
	var wb writeBacks
	trial, err := m.openTrial(ctx, driverName, config, wb.record)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	m.closeTrial(ctx, trial, "试构造")
	if createBucket {
		if err := m.ensureBucket(ctx, driverName, config, wb.record); err != nil {
			return fmt.Errorf("%w: 自动创建存储桶失败(请确认密钥具备建桶权限,或先在控制台建好桶并关闭此项): %w",
				ErrInvalidChannel, err)
		}
	}
	wb.applyTo(config)
	return nil
}

// ensureBucket 对 s3 类渠道主动建桶。
// @Description: 用渠道配置构造一个显式 no_check_bucket=false 的裸驱动(不套 crypt——
// 建桶是桶级操作,只需内层 s3),再调 EnsureBucket。渠道自身仍用默认 no_check_bucket=true,
// 建好一次后续不必每次探测。
// @param drv rclone 后端名
// @param config 已整理的渠道配置(含约定键 path)
// @param onChange 建桶驱动写回的去处
// @return 建桶失败时的错误
func (m *Manager) ensureBucket(ctx context.Context, drv string, config map[string]string,
	onChange func(key, value string)) error {
	params := maps.Clone(config)
	params["no_check_bucket"] = "false"
	d, err := m.openTrial(ctx, drv, params, onChange)
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
// @return ErrChannelNotFound / ErrHoldsSoleReplicas 或落库、重载失败时的错误
func (m *Manager) Delete(ctx context.Context, channelID int64, purge bool) error {
	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := m.cfg.Queries.WithTx(tx)
	if _, err := q.GetChannel(ctx, channelID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrChannelNotFound
		}
		return fmt.Errorf("查询渠道: %w", err)
	}
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
			CreatedAt:       row.CreatedAt,
		})
	}
	return out, nil
}

// probeTimeout 单渠道轻探测超时;各渠道独立计时,慢渠道不拖累其他渠道。
const probeTimeout = 10 * time.Second

// CheckAll 对全部渠道并行做一次健康探测,结果逐渠道落库并更新内存标记。
// @Description: 每轮先按渠道表重载:此前装载失败的渠道在此重试,装上即参与本轮探测,仍失败的
// 原因由 Reload 落库。轻探测 = 只读 Stat 一个约定不存在的对象:ErrNotFound 与成功都视为
// 健康(后端可达),其余错误视为不健康。轻探测只能降级健康渠道,不能独自恢复不健康
// 渠道——失败结论可能来自暴露写路径问题的深探测(如只读凭证),只读成功不构成推翻依据,
// 翻回健康须经深探测确认写读删全通;健康渠道每轮维持零写删成本(周期性写删对按量
// 付费渠道是无谓账单)。各渠道并行、各自计时,整轮耗时等于最慢的一个;ctx 取消
// (停机)时整轮作废不落库——取消造成的探测失败不是渠道的结论。
// 健康翻转只改标记、不重建驱动;每渠道每轮都更新 last_checked_at,失败原因人话化
// 后存 last_error、成功清空。
// @return 本次健康状态翻转的渠道名列表
// @return ctx 已取消、重载或落库失败时的错误;全部渠道无法装载时照常探测旧状态并返回该错误
func (m *Manager) CheckAll(ctx context.Context) ([]string, error) {
	reloadErr := m.Reload(ctx)
	if reloadErr != nil && !errors.Is(reloadErr, errNothingLoaded) {
		return nil, reloadErr
	}
	channels := m.snapshot()
	results := make([]error, len(channels))
	var wg sync.WaitGroup
	for i, c := range channels {
		wg.Go(func() {
			results[i] = probeForHealth(ctx, c.handle.Driver, c.healthy)
		})
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
	return changed, reloadErr
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
	return deepProbe(ctx, d)
}

// probe 在轻探测超时内探测单个渠道可达性(只读不写)。
// @param d 渠道驱动
// @return 渠道不可达时的原因错误;nil 表示健康
func probe(ctx context.Context, d driver.Driver) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return failover.Probe(ctx, d)
}
