package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"
)

// deepProbeTimeout 深探测(写→读→删三次往返)的整体超时。
const deepProbeTimeout = 20 * time.Second

// probeErrorMaxLen 无法归类的探测错误兜底截断长度(按字符计)。
const probeErrorMaxLen = 200

// deepProbe 在深探测整体超时内对驱动执行一次写→读→删探测。
// @param d 目标渠道驱动
// @return 任一环节失败时的原始错误(展示前应经 humanizeProbeError)
func deepProbe(ctx context.Context, d driver.Driver) error {
	ctx, cancel := context.WithTimeout(ctx, deepProbeTimeout)
	defer cancel()
	return failover.DeepProbe(ctx, d)
}

// ProbeChannel 对指定渠道执行深探测并把结果落库。
// @Description: 渠道在库但未装载(此前装载失败)时先按渠道表重试装载:装上即对其深探测,
// 通过后参与选路;仍装不上则失败原因已由 Reload 落库,在渠道列表可见。探测失败不算方法失败——
// 结果体现在 healthy / last_error 里。ctx 取消(请求断开、停机)时不落库并返回取消错误:
// 取消造成的探测失败不是渠道的结论。
// @param channelID 渠道主键
// @return ErrChannelNotFound、ctx 取消或查询、落库失败时的错误
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
	probeErr := deepProbe(ctx, h.Driver)
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.recordProbe(ctx, channelID, probeErr)
}

// unloadedProbeResult 给出重试装载仍未装上的渠道的探测结论:渠道不存在,或装载失败原因已落库。
// @param channelID 渠道主键
// @return ErrChannelNotFound、ctx 取消或查询失败时的错误;装载失败已落库时为 nil
func (m *Manager) unloadedProbeResult(ctx context.Context, channelID int64) error {
	if _, err := m.cfg.Queries.GetChannel(ctx, channelID); errors.Is(err, sql.ErrNoRows) {
		return ErrChannelNotFound
	} else if err != nil {
		return fmt.Errorf("读取渠道 %d: %w", channelID, err)
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

// TestConfig 用一份未落库的渠道配置执行深探测(测试连接)。
// @Description: 不创建渠道、不写任何库表;配置整理规则与 Create / Update 相同。加密属性不参与——
// crypt 密钥派生依赖渠道 ID,本方法验证的是后端连通性与写读删权限。探测失败不算方法失败。
// @param p 测试参数
// @return 探测失败的原因,已人话化并抹除凭证(提交的明文与其编码产物都抹),可直接展示;空串表示连接可用
// @return ErrInvalidChannel(参数非法)、ErrChannelNotFound 或读取渠道失败时的错误
func (m *Manager) TestConfig(ctx context.Context, p TestParams) (string, error) {
	if err := validateCreateParams(CreateParams{
		Name: "connectivity-test", Driver: p.Driver, Config: p.Config,
	}); err != nil {
		return "", err
	}
	var stored map[string]string
	if p.ChannelID > 0 {
		var err error
		if stored, err = m.storedConfig(ctx, p.ChannelID); err != nil {
			return "", err
		}
	}
	prepared, err := prepareConfig(p.Driver, p.Config, stored, p.Template != "")
	if err != nil {
		return "", err
	}
	secret, err := secretKeysOf(p.Driver)
	if err != nil {
		return "", err
	}
	redact := func(msg string) string {
		return redactSecrets(redactSecrets(msg, p.Config, secret), prepared, secret)
	}
	// 补齐授权带出的参数要向后端查询,查询失败与连接失败同属探测结论。
	if err := completeTemplateConfig(ctx, p.Template, p.Config, prepared, stored); err != nil {
		return redact(humanizeProbeError(err)), nil
	}
	d, err := m.openTrial(ctx, p.Driver, prepared)
	if err != nil {
		return redact(humanizeProbeError(err)), nil
	}
	defer m.closeTrial(ctx, d, "测试连接")
	return redact(humanizeProbeError(deepProbe(ctx, d))), nil
}

// recordProbe 把一次探测结果写入渠道行(healthy / last_checked_at / last_error)
// 并同步内存健康标记。
// @Description: 探测已经完成,结果不该因调用方此刻取消(请求断开、停机)而丢失,
// 落库脱离取消、只沿用上下文的值。
// @param channelID 渠道主键
// @param perr 探测错误,nil 表示健康
// @return 落库失败时的错误
func (m *Manager) recordProbe(ctx context.Context, channelID int64, perr error) error {
	return m.recordResult(ctx, channelID, perr == nil, humanizeProbeError(perr))
}

// recordResult 把一条探测结论写入渠道行并同步内存健康标记;结论落库脱离调用方的取消。
// @param channelID 渠道主键
// @param healthy 是否健康
// @param failure 面向用户的失败原因;健康时为空串
// @return 落库失败时的错误
func (m *Manager) recordResult(ctx context.Context, channelID int64, healthy bool, failure string) error {
	if err := m.cfg.Queries.UpdateChannelProbeResult(context.WithoutCancel(ctx), sqlc.UpdateChannelProbeResultParams{
		Healthy:   boolToInt(healthy),
		LastError: sql.NullString{String: failure, Valid: failure != ""},
		ID:        channelID,
	}); err != nil {
		return fmt.Errorf("写回渠道 %d 探测结果: %w", channelID, err)
	}
	m.setHealth(channelID, healthy)
	return nil
}

// probeErrorRules 探测错误分类映射:命中任一子串(不区分大小写)即取对应文案。
// 顺序即匹配优先级,越具体越靠前。
var probeErrorRules = []struct {
	needles []string // 底层错误文本特征(小写)
	message string   // 面向用户的排障文案
}{
	// S3(AWS SDK)的 403 错误文本恒带 "StatusCode: 403",据此与其他后端的 403 区分。
	{[]string{"invalidaccesskeyid", "signaturedoesnotmatch", "statuscode: 403"},
		"认证失败(403):检查 Access Key / Secret Key 及其权限"},
	{[]string{"invalid_client", "unauthorized_client"},
		"应用凭证无效:检查 Client ID / Client Secret 是否与签发令牌的应用一致"},
	{[]string{"invalid_grant"}, "授权已失效:刷新令牌被拒绝,请重新授权"},
	{[]string{"401 unauthorized", "unauthenticated", "invalidauthenticationtoken"},
		"认证失败(401):检查用户名、密码或授权令牌是否有效"},
	{[]string{"accessdenied", "access denied", "403 forbidden"}, "无访问权限(403):检查账号对存储位置的权限"},
	{[]string{"nosuchbucket", "no such bucket", "bucketregionerror"}, "存储桶不存在或无权访问:检查桶名与凭证权限"},
	{[]string{"permanentredirect", "authorizationheadermalformed"}, "区域不匹配:检查所选区域与存储桶实际所在区域"},
	{[]string{"no such host", "server misbehaving"}, "无法解析服务端点域名:检查 endpoint 配置"},
	{[]string{"connection refused", "connection reset"}, "无法连接服务端点:检查 endpoint 地址与端口"},
	{[]string{"certificate", "x509", "tls handshake"}, "TLS 证书校验失败:检查端点证书或改用正确协议"},
	{[]string{"context deadline exceeded", "i/o timeout", "timeout"}, "连接超时:检查端点地址与网络连通性"},
	{[]string{"permission denied", "read-only file system"}, "存储路径无写入权限:检查目录权限"},
}

// humanizeProbeError 把底层探测错误翻成面向用户的排障文案。
// @Description: 按错误特征归类映射;无法归类时截断原文兜底。归类文案不含
// 凭证与连接串;持有明文配置的调用方应再经 redactSecrets 兜底抹除。
// @param err 探测错误,nil 返回空串
// @return 面向用户的文案
func humanizeProbeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "连接超时:检查端点地址与网络连通性"
	}
	lower := strings.ToLower(err.Error())
	for _, rule := range probeErrorRules {
		for _, n := range rule.needles {
			if strings.Contains(lower, n) {
				return rule.message
			}
		}
	}
	return truncateRunes(err.Error(), probeErrorMaxLen)
}

// truncateRunes 把 s 截断到最多 limit 个字符(按 rune 计,多字节文本安全)。
// @param s 原文
// @param limit 字符数上限
// @return 截断后的文本,被截断时追加省略号
func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

// redactSecrets 把配置中敏感字段的值从文案里替换为掩码,兜底防凭证回显。
// @param msg 待清洗文案
// @param config 配置
// @param secret 须保密的键(见 secretKeysOf)
// @return 清洗后的文案
func redactSecrets(msg string, config map[string]string, secret map[string]bool) string {
	for k, v := range config {
		if v != "" && secret[k] {
			msg = strings.ReplaceAll(msg, v, "***")
		}
	}
	return msg
}
