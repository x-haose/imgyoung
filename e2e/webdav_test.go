//go:build e2e

package e2e

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/webdav"
)

// webdav 测试服务器的固定凭证。
const (
	davUser = "dav-user"
	davPass = "dav-secret-pass"
	//nolint:gosec // G101:测试桩值,用于断言错误凭证被拒
	davWrongPass = "错误的密码"
)

// startWebDAVServer 起一个带 Basic 认证的内存 WebDAV 服务器。
// @Description: 认证是本用例的核心断言点——密码若未按 rclone 要求编码入库,
// 后端会把明文当作已编码值解出乱码,认证失败即上传失败。
// @param t 测试句柄
// @return 服务地址
func startWebDAVServer(t *testing.T) string {
	t.Helper()
	dav := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(davUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(davPass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="e2e"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		dav.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestWebDAVChannelRoundTrip WebDAV 渠道端到端:建渠道 → 上传 → 取回字节一致。
// @Description: 覆盖网盘系渠道的完整闭环,顺带证明模板路径下密码编码正确
// (编码错则 WebDAV 服务器返回 401,上传必失败)。
func TestWebDAVChannelRoundTrip(t *testing.T) {
	davURL := startWebDAVServer(t)
	inst := startInstance(t, nil)

	body, err := json.Marshal(map[string]any{
		"name":     "dav-primary",
		"template": "webdav",
		"priority": 500, // 高于内置 local,确保本渠道被选为主渠道
		"inputs": map[string]string{
			"url": davURL + "/", "vendor": "other",
			"user": davUser, "pass": davPass, "path": "imgyoung",
		},
	})
	if err != nil {
		t.Fatalf("编码请求体: %v", err)
	}
	createChannel(t, inst, string(body))

	payload := pngBytes(t)
	resp := upload(t, inst, inst.token, "dav.png", payload, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("上传状态码 = %d, 期望 2xx; body=%s", resp.StatusCode, raw)
	}
	var up struct {
		URL string `json:"url"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &up); err != nil {
		t.Fatalf("解析上传响应: %v; body=%s", err, raw)
	}
	if up.URL == "" {
		t.Fatalf("上传响应无 URL; body=%s", raw)
	}

	parsed, err := url.Parse(up.URL)
	if err != nil {
		t.Fatalf("解析上传返回的 URL: %v", err)
	}
	got := fetchBytes(t, inst, parsed.Path)
	if len(got) != len(payload) {
		t.Fatalf("取回字节数 = %d, 期望 %d", len(got), len(payload))
	}
}

// TestWebDAVChannelRejectsWrongPassword 密码错误时测试连接失败,不留假健康渠道。
func TestWebDAVChannelRejectsWrongPassword(t *testing.T) {
	davURL := startWebDAVServer(t)
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)

	body, err := json.Marshal(map[string]any{
		"name":     "dav-bad",
		"template": "webdav",
		"inputs": map[string]string{
			"url": davURL + "/", "vendor": "other",
			"user": davUser, "pass": davWrongPass, "path": "imgyoung",
		},
	})
	if err != nil {
		t.Fatalf("编码请求体: %v", err)
	}
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels/test", string(body))
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var tr struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("解析测试连接响应: %v; body=%s", err, raw)
	}
	if tr.OK {
		t.Fatal("错误密码的 WebDAV 渠道测试连接却通过了")
	}
	// 失败原因须可读且不回显凭证。
	if tr.Error == "" {
		t.Error("测试连接失败但未给出原因")
	}
	if containsAny(tr.Error, davPass, davWrongPass) {
		t.Errorf("失败原因回显了凭证: %q", tr.Error)
	}
}

// containsAny 报告 s 是否包含任一子串。
// @param s 待检字符串
// @param subs 子串集合
// @return 是否命中
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
