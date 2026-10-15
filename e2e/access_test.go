//go:build e2e

package e2e

import (
	"net/http"
	"testing"
	"time"
)

// headStatus 发一个 HEAD 请求,返回状态码、响应头与读到的正文长度。
func headStatus(t *testing.T, inst *instance, path string) (int, http.Header, int64) {
	t.Helper()
	req, err := http.NewRequest(http.MethodHead, inst.baseURL+path, nil)
	if err != nil {
		t.Fatalf("构造 HEAD 请求: %v", err)
	}
	resp, err := newClient(t).Do(req)
	if err != nil {
		t.Fatalf("发送 HEAD 请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	n, _ := resp.Body.Read(make([]byte, 1))
	return resp.StatusCode, resp.Header, int64(n)
}

// TestAccessCacheHeadersAndHead 短链按一年不可变缓存;对象键别名每次确认(可被同键重传改指);
// 走过格式协商的原图也带 Vary: Accept;HEAD 只给响应头。
func TestAccessCacheHeadersAndHead(t *testing.T) {
	t.Parallel()
	env := s3Setup(t, procSettings)
	inst, cred := env.inst, env.cred

	if err := putWith(t, cred, "alias.png"); err != nil {
		t.Fatalf("S3 上传: %v", err)
	}
	alias := objectPath(inst, cred, "alias.png")
	resp, _ := procGet(t, inst, alias, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("别名响应 = %d, Cache-Control %q, 期望 200 与 public, no-cache",
			resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	cond, _ := procGet(t, inst, alias, map[string]string{"If-None-Match": resp.Header.Get("ETag")})
	if cond.StatusCode != http.StatusNotModified || cond.Header.Get("Cache-Control") != "public, no-cache" {
		t.Errorf("别名条件请求 = %d, Cache-Control %q, 期望 304 与 public, no-cache",
			cond.StatusCode, cond.Header.Get("Cache-Control"))
	}

	_, url := mustUpload(t, inst, procJPEG(64, 48, 0), nil, nil)
	orig, body := procGet(t, inst, url, map[string]string{"Accept": "image/png"})
	if orig.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("短链 Cache-Control = %q, 期望一年 immutable", orig.Header.Get("Cache-Control"))
	}
	if orig.Header.Get("Vary") != "Accept" {
		t.Errorf("协商后仍输出原图时 Vary = %q, 期望 Accept", orig.Header.Get("Vary"))
	}

	code, hd, n := headStatus(t, inst, url)
	if code != http.StatusOK || n != 0 || hd.Get("Content-Length") != orig.Header.Get("Content-Length") {
		t.Errorf("HEAD = %d, 正文 %d 字节, Content-Length %q, 期望 200、无正文、长度与 GET 一致(%d)",
			code, n, hd.Get("Content-Length"), len(body))
	}
	if code, _, _ := headStatus(t, inst, alias); code != http.StatusOK {
		t.Errorf("别名 HEAD = %d, 期望 200", code)
	}
}

// TestExpiredImageNotServed 到期即不可访问,不等清理扫描回收。
func TestExpiredImageNotServed(t *testing.T) {
	t.Parallel()
	// 清理扫描间隔拉到 1 小时,测试期间不会回收,404 只能来自访问路径的到期判定。
	inst := startInstance(t, map[string]any{"cleanup_sweep_seconds": 3600})
	_, url := mustUpload(t, inst, pngBytes(t), map[string]string{"ttl": "1"}, nil)
	waitUntil(t, 10*time.Second, "到期后访问 404", func() bool {
		resp := fetch(t, inst, url, "")
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusNotFound
	})
}
