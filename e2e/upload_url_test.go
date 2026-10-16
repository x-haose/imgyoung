//go:build e2e

// URL 转存的 e2e 覆盖:SSRF 闸的拒绝分支、鉴权,以及幂等键命中时不抓取远端;抓取成功的路径由
// internal/http/router 的集成测试以抓取器替身覆盖(真实抓取器按设计拒绝环回地址)。
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestUploadURLRejectsSSRFTargets URL 转存的 SSRF 闸:非 http/https 协议与内网地址一律拒绝,
// 且不因目标可达与否泄露差异。
func TestUploadURLRejectsSSRFTargets(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)

	targets := map[string]string{
		"file 协议":   "file:///etc/passwd",
		"gopher 协议": "gopher://127.0.0.1:6379/_INFO",
		"环回地址":      "http://127.0.0.1:6379/",
		"云元数据":      "http://169.254.169.254/latest/meta-data/",
		"内网段":       "http://10.0.0.1/a.png",
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			code, body := postUploadURL(t, inst, fmt.Sprintf(`{"url":%q}`, target), nil)
			if code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, 期望 400; body=%s", code, body)
			}
		})
	}
}

// TestUploadURLIdempotencyHitSkipsFetch 幂等键命中时直接返回第一次的结果,不访问远端:目标是环回地址,
// 一旦去抓取就会被 SSRF 闸拒绝。参数仍先于查键校验,非法的 EXIF 档位照样 400。
func TestUploadURLIdempotencyHitSkipsFetch(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	headers := map[string]string{"Idempotency-Key": "e2e-url-retry"}
	first, _ := mustUpload(t, inst, pngBytes(t), nil, headers)

	code, body := postUploadURL(t, inst, `{"url":"http://127.0.0.1:1/a.png"}`, headers)
	if code != http.StatusOK {
		t.Fatalf("命中幂等键的 URL 转存状态码 = %d, 期望 200; body=%s", code, body)
	}
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.ID != first {
		t.Fatalf("命中幂等键应返回第一次的 id %q, 实际 %s (err=%v)", first, body, err)
	}

	code, body = postUploadURL(t, inst, `{"url":"http://127.0.0.1:1/a.png","exif_policy":"bogus"}`, headers)
	if code != http.StatusBadRequest || !strings.Contains(body, `"upload_exif_policy"`) {
		t.Fatalf("非法 EXIF 档位状态码 = %d, 期望 400 upload_exif_policy; body=%s", code, body)
	}
}

// TestUploadURLRequiresToken URL 转存与直传同一道鉴权:无 Token 一律 401。
func TestUploadURLRequiresToken(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	req, err := http.NewRequest(http.MethodPost, inst.baseURL+"/api/upload/url",
		strings.NewReader(`{"url":"https://example.com/a.png"}`))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	resp, err := newClient(t).Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 Token 状态码 = %d, 期望 401", resp.StatusCode)
	}
}

// postUploadURL 以有效 Token 发一次 URL 转存请求。
// @param headers 额外请求头,可为 nil
// @return 状态码与响应体
func postUploadURL(t *testing.T, inst *instance, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, inst.baseURL+"/api/upload/url", strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := newClient(t).Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}
