package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
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

// ErrChannelDeleting 渠道正在清理中:不再编辑、探测或测试,只等排空完成或直接移除。
var ErrChannelDeleting = errors.New("channel: 渠道正在清理中")

// TokenRefreshedError 保存渠道失败,而保存过程中服务商已刷新了表单提交的授权令牌。
// @Description: 刷新令牌只能用一次的服务商(Box 等)此时已作废原令牌,调用方须把新令牌交还表单,
// 下次提交用它;沿用渠道已存令牌的情形不会产生本错误——刷新出的令牌已写回渠道。
type TokenRefreshedError struct {
	Token string // 刷新后的令牌 JSON,与 rclone 的 token 参数同格式
	Err   error  // 保存失败的原因
}

// Error 返回保存失败的原因。
// @return 错误文本
func (e *TokenRefreshedError) Error() string { return e.Err.Error() }

// Unwrap 返回保存失败的原因,调用方仍按它分类。
// @return 保存失败的原因
func (e *TokenRefreshedError) Unwrap() error { return e.Err }

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
// @return 参数非法时包裹 ErrInvalidChannel 的带错误码错误
func validateCreateParams(p CreateParams) error {
	if p.Name == "" || p.Driver == "" {
		return errcode.Wrap(ErrInvalidChannel, errcode.ChannelNameDriverRequired)
	}
	if len(p.Name) > nameMaxLen {
		return errcode.Wrap(ErrInvalidChannel, errcode.ChannelNameTooLong, "max", strconv.Itoa(nameMaxLen))
	}
	if p.Encrypted && p.AllowDirectLink {
		return errcode.Wrap(ErrInvalidChannel, errcode.ChannelEncryptedDirect)
	}
	for k := range p.Config {
		if k == configKeyPath || k == configKeyPublicBaseURL {
			continue
		}
		if !configKeyPattern.MatchString(k) {
			return errcode.Wrap(ErrInvalidChannel, errcode.ChannelConfigKeyInvalid, "key", k)
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
// @return 哨兵落在非敏感键或无原值可还原、后端不存在时包裹 ErrInvalidChannel 的带错误码错误
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
			return nil, errcode.Wrap(ErrInvalidChannel, errcode.ChannelKeepNotSecret, "field", k)
		case stored[k] == "":
			return nil, errcode.Wrap(ErrInvalidChannel, errcode.ChannelKeepNoValue, "field", k)
		default:
			kept[k] = stored[k]
		}
	}
	encode := rclone.ObscurePlainSecrets
	if fromTemplate {
		encode = rclone.ObscureSecrets
	}
	if err := encode(driverName, out); err != nil {
		return nil, errcode.Wrap(ErrInvalidChannel, errcode.ChannelParamInvalid, "detail", err.Error())
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
	Failure         *LastFailure `json:"failure"`           // 最近一次失败的结论;nil 表示最近一次探测成功或尚未出错
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
// 补齐授权或试构造期间服务商刷新了提交的令牌而创建随后失败时,错误以 TokenRefreshedError 交还新令牌。
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
	submitted := config[ConfigKeyToken]
	channelID, err := m.createRecord(ctx, p, config)
	if err != nil {
		return 0, withRefreshedToken(err, submitted, config[ConfigKeyToken])
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

// createRecord 补齐授权带出的参数、试构造验证、落库并播种存量副本;后端写回的参数随时并入 config。
// @param p 创建参数
// @param config 已整理的配置;补齐与试构造期间的写回原地并入,失败时也已并入
// @return 新渠道 ID
// @return 后端初始化失败或落库失败时的错误
func (m *Manager) createRecord(ctx context.Context, p CreateParams, config map[string]string) (int64, error) {
	if err := completeTemplateConfig(ctx, p.Template, p.Config, config, nil); err != nil {
		return 0, backendError(errcode.ChannelBackendFailed, err, config, p.Driver)
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
	return channelID, nil
}

// withRefreshedToken 保存失败时,若服务商已把提交的令牌刷新成新值,把新令牌随错误交还。
// @param err 保存失败的原因
// @param submitted 提交的令牌;空表示没有提交令牌
// @param current 保存过程结束时配置里的令牌
// @return 令牌未变时原样返回 err,否则为 TokenRefreshedError
func withRefreshedToken(err error, submitted, current string) error {
	if current == "" || current == submitted {
		return err
	}
	return &TokenRefreshedError{Token: current, Err: err}
}

// backendError 把后端初始化或建桶失败包装成带错误码的 ErrInvalidChannel,原文抹除配置里的凭证。
// @param code 错误码,文案参数为 reason(FailureReason)与 detail(根因原文)
// @param err 后端错误
// @param config 本次使用的配置
// @param driverName rclone 后端名
// @return 包裹 ErrInvalidChannel 的带错误码错误
func backendError(code errcode.Code, err error, config map[string]string, driverName string) error {
	f := Explain(err, secretsIn(driverName, config)...)
	return errcode.Wrap(ErrInvalidChannel, code, "reason", string(f.Reason), "detail", f.Detail)
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
// 清理中的渠道不可编辑。编辑失败而服务商已刷新了令牌时:沿用的已存令牌刷新后写回渠道并重载
// (已存的那份可能已作废,在用驱动须换用新令牌);表单提交的新令牌以 TokenRefreshedError 交还。
// @param channelID 渠道主键
// @param p 编辑参数
// @return ErrChannelNotFound / ErrChannelDeleting / ErrEncryptedImmutable / ErrChannelNameExists /
// ErrInvalidChannel 或落库、重载失败时的错误
func (m *Manager) Update(ctx context.Context, channelID int64, p UpdateParams) error {
	reload, err := m.updateRecord(ctx, channelID, p)
	if err != nil {
		if !reload {
			return err
		}
		if rerr := m.Reload(ctx); rerr != nil {
			return errors.Join(err, fmt.Errorf("刷新后的令牌已写回渠道,但重载失败: %w", rerr))
		}
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
// @Description: 失败时沿用的已存令牌若已被刷新,新令牌写回渠道,reload 为 true。
// @param channelID 渠道主键
// @param p 编辑参数
// @return reload 渠道配置已改动、调用方须重载;仅 err 非 nil 时为 true
// @return err ErrChannelNotFound / ErrChannelDeleting / ErrEncryptedImmutable / ErrChannelNameExists /
// ErrInvalidChannel、TokenRefreshedError 或落库失败时的错误
func (m *Manager) updateRecord(ctx context.Context, channelID int64, p UpdateParams) (reload bool, err error) {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrChannelNotFound
	}
	if err != nil {
		return false, fmt.Errorf("读取渠道 %d: %w", channelID, err)
	}
	if row.PendingDelete != 0 {
		return false, ErrChannelDeleting
	}
	old, config, err := m.editedConfig(row, p)
	if err != nil {
		return false, err
	}
	submitted := config[ConfigKeyToken]
	err = m.applyEdit(ctx, row, p, old, config)
	refreshed := config[ConfigKeyToken]
	if err == nil || refreshed == "" || refreshed == submitted {
		return false, err
	}
	if p.Config[ConfigKeyToken] != SecretPlaceholder {
		return false, &TokenRefreshedError{Token: refreshed, Err: err}
	}
	if perr := m.persistParamLocked(ctx, channelID, ConfigKeyToken, refreshed); perr != nil {
		return false, errors.Join(err, perr)
	}
	return true, err
}

// applyEdit 补齐授权带出的参数、试构造验证,再整体写回渠道记录;后端写回的参数随时并入 config。
// @Description: 后端名或落库配置(哨兵还原、补齐授权后的参数)与原有的有任何不同,即把绑定时刻重置为当前时刻:
// 决定存储位置的参数因后端而异(如 OneDrive 的 drive_id 属敏感参数),一律按改了位置处理,保证对账 GC
// 不回收新位置上原有的对象。比较在试构造之前,试构造期间令牌刷新的写回不计入。
// @param row 渠道现有记录
// @param p 编辑参数
// @param old 渠道已存的配置
// @param config 已整理的配置;补齐与试构造期间的写回原地并入,失败时也已并入
// @return ErrChannelNameExists / ErrInvalidChannel 或落库失败时的错误
func (m *Manager) applyEdit(ctx context.Context, row sqlc.Channel, p UpdateParams,
	old, config map[string]string) error {
	if err := completeTemplateConfig(ctx, p.Template, p.Config, config, old); err != nil {
		return backendError(errcode.ChannelBackendFailed, err, config, p.Driver)
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
		AllowDirectLink: boolToInt(p.AllowDirectLink), Template: p.Template, BoundAt: boundAt, ID: row.ID,
	}); err != nil {
		if db.IsUniqueViolation(err, "channels.name") {
			return ErrChannelNameExists
		}
		return fmt.Errorf("更新渠道记录: %w", err)
	}
	return nil
}

// editedConfig 校验编辑参数,并把提交的配置整理成可入库形态(哨兵还原、密码编码),不访问后端。
// @param row 渠道现有记录
// @param p 编辑参数
// @return old 渠道已存的配置
// @return config 整理后的配置
// @return ErrEncryptedImmutable / ErrInvalidChannel 或解密已存配置失败时的错误
func (m *Manager) editedConfig(row sqlc.Channel, p UpdateParams) (old, config map[string]string, err error) {
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
	return old, config, nil
}

// verifyConfig 落库前以一次性驱动试构造整理好的配置,可选顺带建桶;试构造期间的写回并入配置(原地修改)。
// @Description: 加密渠道以明文形态试构造——crypt 密钥派生依赖渠道 ID 与创建时刻,包裹层在 Reload 时
// 生效。写回多为访问令牌过期时刷新得到的新令牌,成功时随配置落库;失败时也已并入配置,调用方据此交还
// 或写回新令牌,刷新令牌只能用一次的服务商不因此作废授权。建桶在落库前进行,失败即中止,不留
// 「渠道已建但桶不存在」的坑。
// @param driverName rclone 后端名
// @param config 已整理的配置
// @param createBucket 是否主动建桶(需凭证有建桶权限);仅 s3 有意义
// @return 后端初始化或建桶失败时包裹 ErrInvalidChannel 的带错误码错误
func (m *Manager) verifyConfig(ctx context.Context, driverName string, config map[string]string,
	createBucket bool) error {
	var wb writeBacks
	trial, err := m.openTrial(ctx, driverName, config, wb.record)
	if err != nil {
		wb.applyTo(config)
		return backendError(errcode.ChannelBackendFailed, err, config, driverName)
	}
	m.closeTrial(ctx, trial, "试构造")
	if createBucket {
		if err := m.ensureBucket(ctx, driverName, config, wb.record); err != nil {
			wb.applyTo(config)
			return backendError(errcode.ChannelBucketFailed, err, config, driverName)
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
		return errcode.Wrap(ErrHoldsSoleReplicas, errcode.ChannelSoleReplicas, "count", strconv.FormatInt(sole, 10))
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

// FailureOf 从渠道记录还原最近一次失败的结论。
// @param row 渠道记录
// @return 失败结论;最近一次探测成功或尚未出错时为 nil
func FailureOf(row sqlc.Channel) *LastFailure {
	if !row.LastErrorStage.Valid {
		return nil
	}
	return &LastFailure{
		Stage:   FailureStage(row.LastErrorStage.String),
		Failure: Failure{Reason: FailureReason(row.LastErrorReason.String), Detail: row.LastError.String},
	}
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
			Failure:         FailureOf(row),
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
			results[i] = m.probeForHealth(ctx, c)
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var changed []string
	for i, c := range channels {
		if errors.Is(results[i], ErrChannelDeleting) {
			continue
		}
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
// @param c 渠道运行态
// @return 渠道不可用时的原因错误;nil 表示健康;深探测前渠道已进入清理中时为 ErrChannelDeleting
func (m *Manager) probeForHealth(ctx context.Context, c managed) error {
	if err := probe(ctx, c.handle.Driver); err != nil || c.healthy {
		return err
	}
	probeErr, err := m.deepProbeChannel(ctx, c.handle)
	if err != nil {
		return err
	}
	return probeErr
}

// probe 在轻探测超时内探测单个渠道可达性(只读不写)。
// @param d 渠道驱动
// @return 渠道不可达时的原因错误;nil 表示健康
func probe(ctx context.Context, d driver.Driver) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return failover.Probe(ctx, d)
}
