package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"sync/atomic"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"
)

// deepProbeTimeout 深探测(写→读→删三次往返)的整体超时。
const deepProbeTimeout = 20 * time.Second

// deepProbe 在深探测整体超时内对驱动执行一次写→读→删探测。
// @param d 目标渠道驱动
// @return 任一环节失败时的原始错误(展示前应经 Explain 归类)
func deepProbe(ctx context.Context, d driver.Driver) error {
	ctx, cancel := context.WithTimeout(ctx, deepProbeTimeout)
	defer cancel()
	return failover.DeepProbe(ctx, d)
}

// ProbeChannel 对指定渠道执行深探测并把结果落库。
// @Description: 渠道在库但未装载(此前装载失败)时先按渠道表重试装载:装上即对其深探测,
// 通过后参与选路;仍装不上则失败原因已由 Reload 落库,在渠道列表可见。探测失败不算方法失败——
// 结果体现在 healthy / last_error 里。ctx 取消(请求断开、停机)时不落库并返回取消错误:
// 取消造成的探测失败不是渠道的结论。清理中的渠道不探测。
// @param channelID 渠道主键
// @return ErrChannelNotFound、ErrChannelDeleting、ctx 取消或查询、落库失败时的错误
func (m *Manager) ProbeChannel(ctx context.Context, channelID int64) error {
	h, ok := m.Get(channelID)
	if !ok {
		if err := m.Reload(ctx); err != nil && !errors.Is(err, errNothingLoaded) {
			return err
		}
		if h, ok = m.Get(channelID); !ok {
			return m.unloadedProbeResult(ctx, channelID)
		}
	}
	probeErr, err := m.deepProbeChannel(ctx, h)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.recordProbe(ctx, channelID, probeErr)
}

// deepProbeChannel 对在役渠道做一次深探测;探测在渠道的写入租约内进行,排空不会与它交错。
// @param h 渠道句柄
// @return probeErr 探测失败的原因;nil 表示写读删全通
// @return err 渠道已进入清理中时的 ErrChannelDeleting
func (m *Manager) deepProbeChannel(ctx context.Context, h Handle) (probeErr, err error) {
	release, ok := m.HoldWrites(h.ID)
	if !ok {
		return nil, ErrChannelDeleting
	}
	defer release()
	return deepProbe(ctx, h.Driver), nil
}

// unloadedProbeResult 给出重试装载仍未装上的渠道的探测结论:渠道不存在、正在清理中,或装载失败原因已落库。
// @param channelID 渠道主键
// @return ErrChannelNotFound、ErrChannelDeleting、ctx 取消或查询失败时的错误;装载失败已落库时为 nil
func (m *Manager) unloadedProbeResult(ctx context.Context, channelID int64) error {
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrChannelNotFound
	case err != nil:
		return fmt.Errorf("读取渠道 %d: %w", channelID, err)
	case row.PendingDelete != 0:
		return ErrChannelDeleting
	}
	return ctx.Err()
}

// TestParams 测试连接的参数。
type TestParams struct {
	ChannelID int64             // 编辑场景的渠道主键,配置中的哨兵值还原为该渠道的已存原值;0 = 创建场景
	Template  string            // 配置所用模板 id,空 = 自定义;模板表单的密码类值按明文编码
	Driver    string            // rclone 后端名
	Config    map[string]string // 渠道配置(含约定键 path / public_base_url),可为 nil
}

// TestResult 测试连接的结论。
type TestResult struct {
	Failure *Failure // 探测失败的结论,原文已抹除凭证;nil 表示连接可用
	Token   string   // 测试期间服务商刷新了授权令牌、新令牌又没有写回渠道时为新令牌 JSON,调用方须用它替换表单里的令牌;空串表示无需替换
}

// TestConfig 用一份未落库的渠道配置执行深探测(测试连接)。
// @Description: 不创建渠道;配置整理规则与 Create / Update 相同。加密属性不参与——crypt 密钥派生
// 依赖渠道 ID,本方法验证的是后端连通性与写读删权限。探测失败不算方法失败。访问令牌过期时,补齐授权参数
// 或一次性驱动都会刷新它:编辑场景沿用的已存令牌刷新后写回该渠道(见 trialProbe),其余情形(新提交的令牌、
// 写回渠道失败)交还调用方——刷新令牌只能用一次的服务商,丢掉新令牌即作废原授权,随后的保存必然失败。
// 交还的只有令牌:其余写回的参数保存时会重新得出。
// @param p 测试参数
// @return 测试结论;失败原文抹除凭证(提交的明文与其编码产物都抹)
// @return ErrInvalidChannel(参数非法)、ErrChannelNotFound、ErrChannelDeleting、读取渠道或写回后重载失败时的错误
func (m *Manager) TestConfig(ctx context.Context, p TestParams) (TestResult, error) {
	if err := validateCreateParams(CreateParams{
		Name: "connectivity-test", Driver: p.Driver, Config: p.Config,
	}); err != nil {
		return TestResult{}, err
	}
	var stored map[string]string
	if p.ChannelID > 0 {
		var err error
		if stored, err = m.storedConfig(ctx, p.ChannelID); err != nil {
			return TestResult{}, err
		}
	}
	prepared, err := prepareConfig(p.Driver, p.Config, stored, p.Template != "")
	if err != nil {
		return TestResult{}, err
	}
	secret, err := secretKeysOf(p.Driver)
	if err != nil {
		return TestResult{}, err
	}
	token := prepared[ConfigKeyToken]
	// 补齐授权带出的参数要向后端查询,查询失败与连接失败同属探测结论。
	probeErr := completeTemplateConfig(ctx, p.Template, p.Config, prepared, stored)
	if probeErr == nil {
		var unsaved map[string]string
		if probeErr, unsaved, err = m.trialProbe(ctx, p, prepared); err != nil {
			return TestResult{}, err
		}
		maps.Copy(prepared, unsaved)
	}
	var res TestResult
	if probeErr != nil {
		f := Explain(probeErr, append(secretValues(p.Config, secret), secretValues(prepared, secret)...)...)
		res.Failure = &f
	}
	if refreshed := prepared[ConfigKeyToken]; refreshed != "" && refreshed != token {
		res.Token = refreshed
	}
	return res, nil
}

// trialProbe 用一次性驱动对整理好的配置做深探测,收集后端运行期的写回(访问令牌过期时的刷新)。
// @Description: 编辑场景下,沿用渠道已存值(提交为哨兵)的参数被写回时落到该渠道,随后重载让已装载的驱动
// 换用新值——对刷新令牌只能用一次的服务商,否则库里与在用驱动手里的刷新令牌都已作废。其余写回(本次提交的
// 新值被刷新、渠道配置在测试期间已被编辑,或落库失败)交还调用方。构造或探测失败时写回照样交还:刷新可能
// 发生在失败之前。
// @param p 测试参数
// @param config 已整理的配置
// @return probeErr 构造或探测失败的原因;nil 表示连接可用
// @return unsaved 没有落到渠道的写回,参数名 → 新值
// @return err 渠道已标为清理中时的 ErrChannelDeleting;写回后重载失败时的错误
func (m *Manager) trialProbe(ctx context.Context, p TestParams,
	config map[string]string) (probeErr error, unsaved map[string]string, err error) {
	var persisted atomic.Bool
	var wb writeBacks
	live := newLiveParams(config)
	onChange := func(key, value string) {
		if p.ChannelID > 0 && p.Config[key] == SecretPlaceholder && live.writeBack(key, value,
			func(expected string) bool { return m.persistParam(p.ChannelID, key, expected, value) }) {
			persisted.Store(true)
			return
		}
		wb.record(key, value)
	}
	release, err := m.holdForTrial(p.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	d, probeErr := m.openTrial(ctx, p.Driver, config, onChange)
	if probeErr == nil {
		probeErr = deepProbe(ctx, d)
		m.closeTrial(ctx, d, "测试连接")
	}
	release()
	if persisted.Load() {
		if err := m.Reload(ctx); err != nil && !errors.Is(err, errNothingLoaded) {
			return nil, nil, err
		}
	}
	unsaved = map[string]string{}
	wb.applyTo(unsaved)
	return probeErr, unsaved, nil
}

// holdForTrial 测试已有渠道的配置时占用该渠道的写入租约:试探测与渠道自身的深探测写在同一位置,同样不能与排空交错。
// @param channelID 渠道主键;0 表示创建场景,无须占用
// @return release 探测结束后调用
// @return err 渠道已标为清理中时为 ErrChannelDeleting;渠道未装载(装载失败)时不占用、照常探测
func (m *Manager) holdForTrial(channelID int64) (release func(), err error) {
	if channelID == 0 {
		return func() {}, nil
	}
	if rel, ok := m.HoldWrites(channelID); ok {
		return rel, nil
	}
	if h, found := m.Get(channelID); found && h.PendingDelete {
		return nil, ErrChannelDeleting
	}
	return func() {}, nil
}

// recordProbe 把一次探测结果写入渠道行(healthy / last_checked_at / last_error*)
// 并同步内存健康标记。
// @Description: 探测已经完成,结果不该因调用方此刻取消(请求断开、停机)而丢失,
// 落库脱离取消、只沿用上下文的值。失败原文抹除该渠道的凭证。
// @param channelID 渠道主键
// @param perr 探测错误,nil 表示健康
// @return 落库失败时的错误
func (m *Manager) recordProbe(ctx context.Context, channelID int64, perr error) error {
	if perr == nil {
		return m.recordResult(ctx, channelID, nil)
	}
	return m.recordResult(ctx, channelID, &LastFailure{Stage: StageProbe, Failure: m.ExplainFor(channelID, perr)})
}

// recordResult 把一条探测结论写入渠道行并同步内存健康标记;结论落库脱离调用方的取消。
// @Description: 只写在役渠道:渠道此刻已进入清理中(其失败栏归排空任务)或已删除时不写,也不动内存标记。
// @param channelID 渠道主键
// @param f 失败结论;nil 表示健康
// @return 落库失败时的错误
func (m *Manager) recordResult(ctx context.Context, channelID int64, f *LastFailure) error {
	params := sqlc.UpdateChannelProbeResultParams{Healthy: boolToInt(f == nil), ID: channelID}
	if f != nil {
		params.LastErrorStage = sql.NullString{String: string(f.Stage), Valid: true}
		params.LastErrorReason = sql.NullString{String: string(f.Reason), Valid: true}
		params.LastError = sql.NullString{String: f.Detail, Valid: true}
	}
	rows, err := m.cfg.Queries.UpdateChannelProbeResult(context.WithoutCancel(ctx), params)
	if err != nil {
		return fmt.Errorf("写回渠道 %d 探测结果: %w", channelID, err)
	}
	if rows > 0 {
		m.setHealth(channelID, f == nil)
	}
	return nil
}
