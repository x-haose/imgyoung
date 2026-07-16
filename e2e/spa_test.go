//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// getPath 发送 GET 并返回状态码/Content-Type/响应体。
func getPath(t *testing.T, inst *instance, path string) (int, string, string) {
	t.Helper()
	resp, err := http.Get(inst.baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

// TestSPAServed 冒烟:根路径与前端路由均返回内嵌后台页面,API 未匹配路径保持纯 404。
func TestSPAServed(t *testing.T) {
	inst := startInstance(t, nil)

	for _, path := range []string{"/", "/login", "/images"} {
		code, ct, body := getPath(t, inst, path)
		if code != http.StatusOK || !strings.Contains(ct, "text/html") {
			t.Fatalf("GET %s = %d %q, 期望 200 text/html", path, code, ct)
		}
		if !strings.Contains(body, "ImgYoung") {
			t.Fatalf("GET %s 响应不含后台页面标记", path)
		}
	}
	// SPA 页面必须带内容安全策略。
	resp, err := http.Get(inst.baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("SPA 响应缺少 Content-Security-Policy")
	}

	if code, _, _ := getPath(t, inst, "/api/nonexistent"); code != http.StatusNotFound {
		t.Fatalf("未匹配 API 路径 = %d, 期望 404", code)
	}
}
