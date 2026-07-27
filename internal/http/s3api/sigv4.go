// Package s3api 以 S3 协议接收上传:PicGo/PicList/Typora/快捷指令等客户端普遍内置
// "S3 兼容存储"选项,认这套协议等于一次性接上整个生态,无需为每个客户端写插件。
// 本包只做协议翻译与凭证校验,落库与净化仍走同一条上传管道。
package s3api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SigV4 校验失败的原因哨兵;对外一律翻译成 S3 错误码,不回显判定细节。
var (
	// ErrMalformedAuth Authorization 头缺失或结构不合 SigV4 规范。
	ErrMalformedAuth = errors.New("s3api: Authorization 头格式非法")
	// ErrUnsupportedAlgorithm 签名算法不是 AWS4-HMAC-SHA256。
	ErrUnsupportedAlgorithm = errors.New("s3api: 不支持的签名算法")
	// ErrSignatureMismatch 重算签名与客户端签名不一致。
	ErrSignatureMismatch = errors.New("s3api: 签名不匹配")
	// ErrRequestExpired 请求时间戳超出允许的时钟偏移窗口。
	ErrRequestExpired = errors.New("s3api: 请求时间戳超出允许范围")
)

// sigV4Algorithm 本实现认可的唯一签名算法。
const sigV4Algorithm = "AWS4-HMAC-SHA256"

// maxClockSkew 允许的客户端时钟偏移;超出即拒绝,防止签名被长期重放。
const maxClockSkew = 15 * time.Minute

// amzDateLayout x-amz-date 的时间格式(ISO8601 basic)。
const amzDateLayout = "20060102T150405Z"

// unsignedPayload 客户端声明"正文不参与签名"时的哨兵值。
const unsignedPayload = "UNSIGNED-PAYLOAD"

// authParts 从 Authorization 头解析出的签名要素。
type authParts struct {
	accessKeyID   string   // 凭证标识,用于查出对应密钥
	date          string   // 凭证作用域中的日期(YYYYMMDD)
	region        string   // 凭证作用域中的区域;本服务不区分区域,原样参与计算
	service       string   // 凭证作用域中的服务名,恒为 s3
	signedHeaders []string // 参与签名的头名(小写、已排序),须原样用于重算
	signature     string   // 客户端计算出的签名
}

// parseAuthorization 解析 SigV4 的 Authorization 头。
// @Description: 形如 `AWS4-HMAC-SHA256 Credential=AK/20260727/us-east-1/s3/aws4_request,
// SignedHeaders=host;x-amz-date, Signature=abc`。SignedHeaders 必须按客户端给的原样使用——
// 服务端自行推断该签哪些头,是这类实现最常见的错源。
// @param header Authorization 头原文
// @return 解析出的签名要素
// @return ErrMalformedAuth / ErrUnsupportedAlgorithm
func parseAuthorization(header string) (authParts, error) {
	rest, ok := strings.CutPrefix(header, sigV4Algorithm+" ")
	if !ok {
		if strings.TrimSpace(header) == "" {
			return authParts{}, ErrMalformedAuth
		}
		return authParts{}, ErrUnsupportedAlgorithm
	}
	fields := map[string]string{}
	for _, part := range strings.Split(rest, ",") {
		k, v, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return authParts{}, ErrMalformedAuth
		}
		fields[k] = v
	}
	credential, signedHeaders, signature := fields["Credential"], fields["SignedHeaders"], fields["Signature"]
	if credential == "" || signedHeaders == "" || signature == "" {
		return authParts{}, ErrMalformedAuth
	}
	// Credential = <AK>/<date>/<region>/<service>/aws4_request;AK 自身不含斜杠。
	scope := strings.Split(credential, "/")
	if len(scope) != 5 || scope[4] != "aws4_request" || scope[3] != "s3" {
		return authParts{}, ErrMalformedAuth
	}
	return authParts{
		accessKeyID:   scope[0],
		date:          scope[1],
		region:        scope[2],
		service:       scope[3],
		signedHeaders: strings.Split(signedHeaders, ";"),
		signature:     signature,
	}, nil
}

// verify 用给定密钥重算签名并与客户端签名比对。
// @Description: 按 SigV4 规范重建规范请求 → 待签字符串 → 逐级派生签名密钥;
// 比对用常数时间,避免按字节泄露正确签名。正文哈希取客户端声明的
// x-amz-content-sha256,调用方须另行校验它与实际正文一致,否则正文可被掉包。
// @param r 收到的请求
// @param parts 已解析的签名要素
// @param secret 该 access key 对应的密钥
// @param payloadHash 正文哈希(十六进制)或 UNSIGNED-PAYLOAD
// @return 签名不符时返回 ErrSignatureMismatch
func verify(r *http.Request, parts authParts, secret, payloadHash string) error {
	canonical := canonicalRequest(r, parts.signedHeaders, payloadHash)
	scope := strings.Join([]string{parts.date, parts.region, parts.service, "aws4_request"}, "/")
	sum := sha256.Sum256([]byte(canonical))
	stringToSign := strings.Join([]string{
		sigV4Algorithm,
		amzDate(r),
		scope,
		hex.EncodeToString(sum[:]),
	}, "\n")

	key := signingKey(secret, parts.date, parts.region, parts.service)
	want := hex.EncodeToString(hmacSHA256(key, stringToSign))
	if !hmac.Equal([]byte(want), []byte(parts.signature)) {
		return ErrSignatureMismatch
	}
	return nil
}

// canonicalRequest 构造 SigV4 规范请求串。
// @param r 收到的请求
// @param signedHeaders 客户端声明参与签名的头名(小写)
// @param payloadHash 正文哈希或 UNSIGNED-PAYLOAD
// @return 规范请求串
func canonicalRequest(r *http.Request, signedHeaders []string, payloadHash string) string {
	names := append([]string(nil), signedHeaders...)
	sort.Strings(names)

	var headers strings.Builder
	for _, name := range names {
		headers.WriteString(name)
		headers.WriteByte(':')
		headers.WriteString(canonicalHeaderValue(r, name))
		headers.WriteByte('\n')
	}
	return strings.Join([]string{
		r.Method,
		canonicalURI(r.URL),
		canonicalQuery(r.URL),
		headers.String(),
		strings.Join(names, ";"),
		payloadHash,
	}, "\n")
}

// canonicalHeaderValue 取参与签名的头值并按规范折叠空白。
// @Description: host 与 content-length 在 Go 里都不进 Header map(分别落在 r.Host 与
// r.ContentLength),而客户端签名时把它们当普通头签了进去;漏掉任一条,所有真实客户端的
// 签名都会对不上。
// @param r 收到的请求
// @param name 头名(小写)
// @return 规范化后的头值
func canonicalHeaderValue(r *http.Request, name string) string {
	switch name {
	case "host":
		return strings.TrimSpace(r.Host)
	case "content-length":
		if r.ContentLength >= 0 {
			return strconv.FormatInt(r.ContentLength, 10)
		}
		return ""
	}
	values := r.Header.Values(http.CanonicalHeaderKey(name))
	trimmed := make([]string, 0, len(values))
	for _, v := range values {
		trimmed = append(trimmed, strings.Join(strings.Fields(v), " "))
	}
	return strings.Join(trimmed, ",")
}

// canonicalURI 取规范化的路径。
// @Description: 用 EscapedPath 保持客户端签名时所用的转义形态;S3 的对象键路径段
// 不做二次编码(官方 S3 客户端一律带 DisableURIPathEscaping)。
// @param u 请求地址
// @return 规范路径
func canonicalURI(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// canonicalQuery 构造规范查询串(按编码后的键值排序)。
// @Description: 按 RFC3986 编码:QueryEscape 把空格编成 `+`,而 SigV4 要求 `%20`,
// 不换掉这一处则任何带空格的查询参数都会签名失配。
// @param u 请求地址
// @return 规范查询串
func canonicalQuery(u *url.URL) string {
	values := u.Query()
	parts := make([]string, 0, len(values))
	for k, vs := range values {
		for _, v := range vs {
			parts = append(parts, rfc3986Escape(k)+"="+rfc3986Escape(v))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

// rfc3986Escape 按 RFC3986 转义查询串成分。
// @param s 待转义文本
// @return 转义结果
func rfc3986Escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// signingKey 按 SigV4 逐级派生签名密钥。
// @param secret 凭证密钥
// @param date 凭证作用域日期(YYYYMMDD)
// @param region 区域
// @param service 服务名
// @return 最终签名密钥
func signingKey(secret, date, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	return hmacSHA256(k, "aws4_request")
}

// hmacSHA256 计算 HMAC-SHA256。
// @param key 密钥
// @param data 待签数据
// @return 摘要
func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// amzDate 取请求的签名时间戳原文。
// @Description: 优先 x-amz-date,其次标准 Date 头——两者都是客户端签名时用的那一个,
// 取错会让重算的待签字符串与客户端不一致。
// @param r 收到的请求
// @return 时间戳原文
func amzDate(r *http.Request) string {
	if v := r.Header.Get("X-Amz-Date"); v != "" {
		return v
	}
	return r.Header.Get("Date")
}

// checkClockSkew 校验请求时间戳在允许的偏移窗口内。
// @param r 收到的请求
// @param now 当前时刻
// @return 超窗或时间戳非法时返回 ErrRequestExpired
func checkClockSkew(r *http.Request, now time.Time) error {
	ts, err := time.Parse(amzDateLayout, amzDate(r))
	if err != nil {
		return ErrRequestExpired
	}
	if diff := now.Sub(ts); diff > maxClockSkew || diff < -maxClockSkew {
		return fmt.Errorf("%w: 偏移 %s", ErrRequestExpired, now.Sub(ts).Round(time.Second))
	}
	return nil
}
