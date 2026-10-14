package s3api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	signer "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// 测试用凭证;派生逻辑在 auth 包,本包只关心"给定密钥能否验签"。
const (
	testAK     = "imgy_abc1234"
	testSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// signedRequest 用官方签名器造一个已签名的 PUT 请求。
// @Description: 用真实客户端的签名器而非自造签名,才能证明本实现与 PicGo/awscli/SDK
// 这类客户端互通——服务端自签自验的测试对互操作性零信息量。
// @param body 请求正文
// @param when 签名时刻
// @return 已签名的请求
func signedRequest(t *testing.T, target string, body []byte, when time.Time) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	req.Header.Set(contentSHAHeader, payloadHash)
	req.Header.Set("Content-Type", "image/png")

	creds := aws.Credentials{AccessKeyID: testAK, SecretAccessKey: testSecret}
	// S3 规范不对路径做二次转义,官方 S3 客户端一律带此选项;不设则签出的是非 S3 语义的签名。
	sig := signer.NewSigner(func(o *signer.SignerOptions) { o.DisableURIPathEscaping = true })
	if err := sig.SignHTTP(context.Background(), creds, req, payloadHash, "s3", "us-east-1", when); err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	// 服务端读到的是 r.Host,httptest 之外需手动对齐签名时用的 Host。
	req.Host = req.URL.Host
	return req
}

// TestVerifyAcceptsOfficialSignature 官方签名器签出的请求必须验签通过。
func TestVerifyAcceptsOfficialSignature(t *testing.T) {
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	body := []byte("png-bytes")
	req := signedRequest(t, "https://img.example.com/s3/mybucket/2026/07/a.png", body, now)

	parts, err := parseAuthorization(req.Header.Get("Authorization"))
	if err != nil {
		t.Fatalf("解析 Authorization: %v", err)
	}
	if parts.accessKeyID != testAK {
		t.Errorf("access key = %q, 期望 %q", parts.accessKeyID, testAK)
	}
	if err := verify(req, parts, testSecret, req.Header.Get(contentSHAHeader)); err != nil {
		t.Fatalf("官方签名应通过校验: %v", err)
	}
}

// TestVerifyAcceptsSignedQueryString 带查询参数(含空格与需转义字符)的请求同样验签通过:
// 查询串规范化用 %20 而非 +,编错则所有带空格参数的请求都会失配。
func TestVerifyAcceptsSignedQueryString(t *testing.T) {
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	for _, query := range []string{
		"x-id=PutObject",
		"tag=hello+world",
		"tag=hello%20world",
		"b=2&a=1",
		"name=%E5%9B%BE%E7%89%87",
	} {
		t.Run(query, func(t *testing.T) {
			req := signedRequest(t, "https://img.example.com/s3/b/a.png?"+query, []byte("x"), now)
			parts, err := parseAuthorization(req.Header.Get("Authorization"))
			if err != nil {
				t.Fatalf("解析 Authorization: %v", err)
			}
			if err := verify(req, parts, testSecret, req.Header.Get(contentSHAHeader)); err != nil {
				t.Fatalf("带查询串的官方签名应通过校验: %v", err)
			}
		})
	}
}

// TestVerifyRejectsWrongSecret 密钥不对即拒绝——这是凭证校验的根本断言。
func TestVerifyRejectsWrongSecret(t *testing.T) {
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	req := signedRequest(t, "https://img.example.com/s3/b/a.png", []byte("x"), now)
	parts, err := parseAuthorization(req.Header.Get("Authorization"))
	if err != nil {
		t.Fatalf("解析 Authorization: %v", err)
	}
	wrong := strings.Repeat("f", len(testSecret))
	if err := verify(req, parts, wrong, req.Header.Get(contentSHAHeader)); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("错误密钥期望 ErrSignatureMismatch,得 %v", err)
	}
}

// TestVerifyRejectsTamperedRequest 签名覆盖的要素被改动后一律失配:
// 路径(对象键)、查询串、参与签名的头、HTTP 方法、Host。
func TestVerifyRejectsTamperedRequest(t *testing.T) {
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	body := []byte("png-bytes")

	cases := map[string]func(*http.Request){
		"改对象键":   func(r *http.Request) { r.URL.Path = "/s3/mybucket/evil.png" },
		"加查询参数":  func(r *http.Request) { r.URL.RawQuery = "acl=public-read" },
		"改签名头":   func(r *http.Request) { r.Header.Set("Content-Type", "image/gif") },
		"改方法":    func(r *http.Request) { r.Method = http.MethodDelete },
		"改 Host": func(r *http.Request) { r.Host = "evil.example.com" },
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			req := signedRequest(t, "https://img.example.com/s3/mybucket/a.png", body, now)
			parts, err := parseAuthorization(req.Header.Get("Authorization"))
			if err != nil {
				t.Fatalf("解析 Authorization: %v", err)
			}
			tamper(req)
			err = verify(req, parts, testSecret, req.Header.Get(contentSHAHeader))
			if !errors.Is(err, ErrSignatureMismatch) {
				t.Fatalf("篡改后期望 ErrSignatureMismatch,得 %v", err)
			}
		})
	}
}

// TestParseAuthorizationRejectsMalformed 结构不合规的 Authorization 头一律拒绝,不做宽容解析。
func TestParseAuthorizationRejectsMalformed(t *testing.T) {
	cases := map[string]struct {
		header string
		want   error
	}{
		"空头": {"", ErrMalformedAuth},
		"其他算法": {
			"AWS1-HMAC-SHA1 Credential=a/b/c/s3/aws4_request,SignedHeaders=host,Signature=x", ErrUnsupportedAlgorithm,
		},
		"缺签名": {
			sigV4Algorithm + " Credential=ak/20260727/us-east-1/s3/aws4_request,SignedHeaders=host", ErrMalformedAuth,
		},
		"作用域不完整": {sigV4Algorithm + " Credential=ak/20260727/s3,SignedHeaders=host,Signature=x", ErrMalformedAuth},
		"非 s3 服务": {
			sigV4Algorithm + " Credential=ak/20260727/us-east-1/ec2/aws4_request,SignedHeaders=host,Signature=x",
			ErrMalformedAuth,
		},
		"作用域尾段错": {
			sigV4Algorithm + " Credential=ak/20260727/us-east-1/s3/aws5_request,SignedHeaders=host,Signature=x",
			ErrMalformedAuth,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAuthorization(tc.header); !errors.Is(err, tc.want) {
				t.Errorf("期望 %v,得 %v", tc.want, err)
			}
		})
	}
}

// TestCheckClockSkew 时间戳超出偏移窗口即拒绝,防签名被长期重放。
func TestCheckClockSkew(t *testing.T) {
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		stamp   time.Time
		wantErr bool
	}{
		"同刻":   {now, false},
		"窗口内":  {now.Add(-10 * time.Minute), false},
		"轻微超前": {now.Add(5 * time.Minute), false},
		"过旧":   {now.Add(-30 * time.Minute), true},
		"过新":   {now.Add(30 * time.Minute), true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/s3/b/a.png", nil)
			req.Header.Set("X-Amz-Date", tc.stamp.UTC().Format(amzDateLayout))
			err := checkClockSkew(req, now)
			if tc.wantErr != (err != nil) {
				t.Fatalf("时间戳 %s: err = %v, 期望出错 = %t", tc.stamp, err, tc.wantErr)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPut, "/s3/b/a.png", nil)
	req.Header.Set("X-Amz-Date", "not-a-date")
	if err := checkClockSkew(req, now); !errors.Is(err, ErrRequestExpired) {
		t.Errorf("非法时间戳期望 ErrRequestExpired,得 %v", err)
	}
}

// TestVerifyPayloadDetectsSwappedBody 声明的正文哈希与实际正文不符即拒绝:
// 签名只保证"这份哈希是我签的",正文本身还得对得上这份哈希。
func TestVerifyPayloadDetectsSwappedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/s3/b/a.png", nil)
	sum := sha256.Sum256([]byte("original"))
	req.Header.Set(contentSHAHeader, hex.EncodeToString(sum[:]))

	if err := verifyPayload(req, []byte("original")); err != nil {
		t.Fatalf("哈希相符不应报错: %v", err)
	}
	if err := verifyPayload(req, []byte("swapped")); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("正文被换期望 ErrSignatureMismatch,得 %v", err)
	}

	// 客户端声明不签正文时跳过比对(该模式下正文本就不在签名保护内)。
	req.Header.Set(contentSHAHeader, unsignedPayload)
	if err := verifyPayload(req, []byte("anything")); err != nil {
		t.Errorf("UNSIGNED-PAYLOAD 不应比对正文: %v", err)
	}
}

// TestObjectKeyFromPath 对象键取协议面前缀与桶名之后的整段路径,含斜杠与转义字符;
// 控制字符、前导斜杠与超长键一律拒绝。
func TestObjectKeyFromPath(t *testing.T) {
	cases := map[string]struct {
		path string
		want string
		ok   bool
	}{
		"单层键":     {"/s3/bucket/a.png", "a.png", true},
		"多层键":     {"/s3/bucket/2026/07/a.png", "2026/07/a.png", true},
		"转义空格":    {"/s3/bucket/my%20photo.png", "my photo.png", true},
		"转义斜杠即分隔": {"/s3/bucket/a%2Fb.png", "a/b.png", true},
		"中文键":     {"/s3/bucket/%E5%9B%BE.png", "图.png", true},
		"只有桶名":    {"/s3/bucket", "", false},
		"桶名带尾斜杠":  {"/s3/bucket/", "", false},
		"缺协议面前缀":  {"/bucket/a.png", "", false},
		"前导斜杠":    {"/s3/bucket/%2Fetc/passwd", "", false},
		"含控制字符":   {"/s3/bucket/a%0Ab.png", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := objectKeyFromPath(tc.path)
			if tc.ok != (err == nil) {
				t.Fatalf("%s: err = %v, 期望成功 = %t", tc.path, err, tc.ok)
			}
			if tc.ok && got != tc.want {
				t.Errorf("对象键 = %q, 期望 %q", got, tc.want)
			}
		})
	}
	_, err := objectKeyFromPath("/s3/bucket/" + strings.Repeat("k", maxObjectKeyBytes+1))
	if !errors.Is(err, errKeyTooLong) {
		t.Error("超长对象键应被拒")
	}
}
