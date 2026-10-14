//go:build e2e

// URL 转存(迭代 8)的 e2e 覆盖:SSRF 闸的拒绝分支与鉴权;成功路径由
// internal/http/router 的集成测试以抓取器替身覆盖(真实抓取器按设计拒绝环回地址)。
package e2e

import (
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
			code, body := postUploadURL(t, inst, fmt.Sprintf(`{"url":%q}`, target))
			if code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, 期望 400; body=%s", code, body)
			}
		})
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
// @return 状态码与响应体
func postUploadURL(t *testing.T, inst *instance, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, inst.baseURL+"/api/upload/url", strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := newClient(t).Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}
