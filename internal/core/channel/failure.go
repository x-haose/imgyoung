package channel

import (
	"context"
	"errors"
	"strings"

	"github.com/x-haose/imgyoung/store/failover"
)

// FailureReason 渠道上一次操作失败的归类;取值即接口契约里 FailureReason 枚举的成员,管理后台按它取当前语言的说明。
type FailureReason string

// 失败归类。按底层错误的特征判定,判不出时为 ReasonUnclassified,排障看根因原文。
const (
	ReasonS3Auth             FailureReason = "s3_auth"             // S3 拒绝凭证(403):Access Key / Secret Key 错误或无权限
	ReasonAppCredentials     FailureReason = "app_credentials"     // OAuth 应用凭证(Client ID / Secret)与签发令牌的应用不一致
	ReasonGrantRevoked       FailureReason = "grant_revoked"       // 刷新令牌被服务商拒绝,须重新授权
	ReasonAuth               FailureReason = "auth"                // 认证失败(401):用户名、密码或授权令牌无效
	ReasonForbidden          FailureReason = "forbidden"           // 无访问权限(403)
	ReasonBucketMissing      FailureReason = "bucket_missing"      // 存储桶不存在或无权访问
	ReasonRegionMismatch     FailureReason = "region_mismatch"     // 所选区域与存储桶实际所在区域不符
	ReasonDNS                FailureReason = "dns"                 // 服务端点域名无法解析
	ReasonConnect            FailureReason = "connect"             // 服务端点拒绝或重置连接
	ReasonTLS                FailureReason = "tls"                 // TLS 证书校验失败
	ReasonTimeout            FailureReason = "timeout"             // 连接或操作超时
	ReasonPermission         FailureReason = "permission"          // 存储路径无写入权限(本地目录)
	ReasonProbeMismatch      FailureReason = "probe_mismatch"      // 探测对象读回的内容与写入的不一致
	ReasonConfigUnreadable   FailureReason = "config_unreadable"   // 渠道配置无法解密或解析,多为主密钥与写入时不同
	ReasonNoSource           FailureReason = "no_source"           // 补齐副本时没有可读的源副本
	ReasonChannelUnavailable FailureReason = "channel_unavailable" // 补齐的目标渠道未装载
	ReasonObjectMissing      FailureReason = "object_missing"      // 对账发现记为就绪的副本在渠道上已不存在
	ReasonUnclassified       FailureReason = "unclassified"        // 无法归类
)

// FailureStage 渠道失败结论出自哪一步。
type FailureStage string

// 失败环节。
const (
	StageProbe FailureStage = "probe" // 健康探测
	StageLoad  FailureStage = "load"  // 按配置装载驱动
	StageDrain FailureStage = "drain" // 删除清理中渠道上的对象
)

// Failure 一次失败的结论。
type Failure struct {
	Reason FailureReason `json:"reason"` // 归类
	Detail string        `json:"detail"` // 根因原文,已抹除凭证、截断;多为服务商或网络库的英文原文
}

// LastFailure 渠道最近一次失败的结论:失败环节加归类与根因原文。
type LastFailure struct {
	Stage   FailureStage `json:"stage"` // 失败环节
	Failure              // 归类与根因原文
}

// reasonMessages 各归类的中文说明,用于服务端自己产出的文案(告警描述、Webhook);管理后台按归类查自己的词典。
var reasonMessages = map[FailureReason]string{
	ReasonS3Auth:             "认证失败(403):检查 Access Key / Secret Key 及其权限",
	ReasonAppCredentials:     "应用凭证无效:检查 Client ID / Client Secret 是否与签发令牌的应用一致",
	ReasonGrantRevoked:       "授权已失效:刷新令牌被拒绝,请重新授权",
	ReasonAuth:               "认证失败(401):检查用户名、密码或授权令牌是否有效",
	ReasonForbidden:          "无访问权限(403):检查账号对存储位置的权限",
	ReasonBucketMissing:      "存储桶不存在或无权访问:检查桶名与凭证权限",
	ReasonRegionMismatch:     "区域不匹配:检查所选区域与存储桶实际所在区域",
	ReasonDNS:                "无法解析服务端点域名:检查 endpoint 配置",
	ReasonConnect:            "无法连接服务端点:检查 endpoint 地址与端口",
	ReasonTLS:                "TLS 证书校验失败:检查端点证书或改用正确协议",
	ReasonTimeout:            "连接超时:检查端点地址与网络连通性",
	ReasonPermission:         "存储路径无写入权限:检查目录权限",
	ReasonProbeMismatch:      "探测对象读回的内容与写入的不一致:存储端改写或截断了对象",
	ReasonConfigUnreadable:   "渠道配置无法解密:当前主密钥与保存配置时用的不同",
	ReasonNoSource:           "没有可读的源副本",
	ReasonChannelUnavailable: "目标渠道未装载",
	ReasonObjectMissing:      "对账发现渠道上缺少该对象",
	ReasonUnclassified:       "未能归类的错误",
}

// stagePrefixes 各失败环节在中文说明前的前缀。
var stagePrefixes = map[FailureStage]string{
	StageProbe: "",
	StageLoad:  "装载失败:",
	StageDrain: "清理渠道对象失败,会自动重试;凭证已失效或存储桶已不存在时可直接移除(远端对象保留):",
}

// Message 返回中文说明:归类说明加根因原文;无法归类时只有原文。
// @return 说明
func (f Failure) Message() string {
	switch {
	case f.Detail == "":
		return reasonMessages[f.Reason]
	case f.Reason == ReasonUnclassified:
		return f.Detail
	default:
		return reasonMessages[f.Reason] + "(" + f.Detail + ")"
	}
}

// Message 返回中文说明:环节前缀、归类说明加根因原文。
// @return 说明
func (f LastFailure) Message() string {
	return stagePrefixes[f.Stage] + f.Failure.Message()
}

// failureDetailMaxLen 根因原文的截断长度(按字符计)。
const failureDetailMaxLen = 200

// configError 渠道配置密文无法解密或解析,多为主密钥与写入时不同。
type configError struct {
	err error // 解密或解析的原始错误
}

// Error 返回带步骤说明的错误文本。
// @return 错误文本
func (e configError) Error() string { return "渠道配置无法读取: " + e.err.Error() }

// Unwrap 返回原始错误。
// @return 原始错误
func (e configError) Unwrap() error { return e.err }

// failureRules 按底层错误文本特征归类:命中任一子串(不区分大小写)即取对应归类。
// 顺序即匹配优先级,越具体越靠前。
var failureRules = []struct {
	needles []string      // 底层错误文本特征(小写)
	reason  FailureReason // 归类
}{
	// S3(AWS SDK)的 403 错误文本恒带 "StatusCode: 403",据此与其他后端的 403 区分。
	{[]string{"invalidaccesskeyid", "signaturedoesnotmatch", "statuscode: 403"}, ReasonS3Auth},
	{[]string{"invalid_client", "unauthorized_client"}, ReasonAppCredentials},
	{[]string{"invalid_grant"}, ReasonGrantRevoked},
	{[]string{"401 unauthorized", "unauthenticated", "invalidauthenticationtoken"}, ReasonAuth},
	{[]string{"accessdenied", "access denied", "403 forbidden"}, ReasonForbidden},
	{[]string{"nosuchbucket", "no such bucket", "bucketregionerror"}, ReasonBucketMissing},
	{[]string{"permanentredirect", "authorizationheadermalformed"}, ReasonRegionMismatch},
	{[]string{"no such host", "server misbehaving"}, ReasonDNS},
	{[]string{"connection refused", "connection reset"}, ReasonConnect},
	// 握手超时是网络问题,文本里也带 "tls handshake",须排在证书规则之前。
	{[]string{"tls handshake timeout"}, ReasonTimeout},
	{[]string{"certificate", "x509", "tls handshake"}, ReasonTLS},
	{[]string{"context deadline exceeded", "i/o timeout", "timeout"}, ReasonTimeout},
	{[]string{"permission denied", "read-only file system"}, ReasonPermission},
}

// Explain 把一个底层错误归类为失败结论。
// @Description: 归类看整条错误链的文本;原文只取链上最内层的错误——外层是本服务逐层加的步骤说明,
// 最内层通常是服务商或网络库的原文。原文里出现 secrets 中任一值即替换为 ***,再截断。
// @param err 底层错误,不可为 nil
// @param secrets 须从原文里抹除的值(凭证明文与其编码产物);空串忽略
// @return 失败结论
func Explain(err error, secrets ...string) Failure {
	detail := rootCause(err).Error()
	for _, s := range secrets {
		if s != "" {
			detail = strings.ReplaceAll(detail, s, "***")
		}
	}
	return Failure{Reason: classify(err), Detail: truncateRunes(detail, failureDetailMaxLen)}
}

// classify 按错误链与文本特征给出归类。
// @param err 底层错误
// @return 归类
func classify(err error) FailureReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	case errors.Is(err, failover.ErrProbeMismatch):
		return ReasonProbeMismatch
	}
	if _, ok := errors.AsType[configError](err); ok {
		return ReasonConfigUnreadable
	}
	lower := strings.ToLower(err.Error())
	for _, rule := range failureRules {
		for _, n := range rule.needles {
			if strings.Contains(lower, n) {
				return rule.reason
			}
		}
	}
	return ReasonUnclassified
}

// rootCause 沿单链 Unwrap 取最内层的错误;遇到包裹多个错误的节点即停在该节点。
// @param err 错误
// @return 最内层错误
func rootCause(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// truncateRunes 把 s 截断到最多 limit 个字符(按 rune 计,多字节文本安全)。
// @param s 原文
// @param limit 字符数上限,>= 1
// @return 截断后的文本,不超过 limit 个字符;被截断时末字符为省略号
func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit-1]) + "…"
}

// secretValues 取配置里须保密的值,供 Explain 从原文里抹除。
// @param config 配置
// @param secret 须保密的键(见 secretKeysOf)
// @return 值切片
func secretValues(config map[string]string, secret map[string]bool) []string {
	out := make([]string, 0, len(secret))
	for k, v := range config {
		if secret[k] && v != "" {
			out = append(out, v)
		}
	}
	return out
}
