package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/referer"
	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// fetchStatus 以指定 Referer 访问 URL,返回状态码。
func fetchStatus(t *testing.T, ts *httptest.Server, url, referrer string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("构造访问请求: %v", err)
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer closeBody(t, resp)
	return resp.StatusCode
}

// refererListResp 规则列表响应的断言用投影。
type refererListResp struct {
	Items []struct {
		ID      int64  `json:"id"`
		Pattern string `json:"pattern"`
	} `json:"items"`
	Total int64 `json:"total"`
}

// TestRefererWhitelistHotReload 验证防盗链 DB 化热生效闭环:
// 管理 API 加规则后坏 Referer 立即 403(无需重启),删规则后立即放行,
// 变更均落审计;重启进程(以同库重建服务)规则仍在。
func TestRefererWhitelistHotReload(t *testing.T) {
	var env testEnv
	ts := newTestServer(t, func(_ *testConfig, e testEnv) { env = e })
	client, csrf := adminSession(t, ts)

	up := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, up)
	_, imgURL := decodeUpload(t, up)
	imgFull := ts.URL + imgURL

	// 无规则:任意 Referer 放行。
	if code := fetchStatus(t, ts, imgFull, "https://evil.io/steal"); code != http.StatusOK {
		t.Fatalf("无规则时状态码 = %d, 期望 200", code)
	}

	// 加规则(大小写归一)→ 即时生效:坏 Referer 403,命中放行。
	created := adminReq(t, client, csrf, http.MethodPost, ts.URL+"/api/admin/referer-whitelist",
		`{"pattern":" *.OK.com "}`)
	defer closeBody(t, created)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("添加规则状态码 = %d, 期望 201", created.StatusCode)
	}
	var rule struct {
		ID      int64  `json:"id"`
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(created.Body).Decode(&rule); err != nil {
		t.Fatalf("解析添加响应: %v", err)
	}
	if rule.Pattern != "*.ok.com" {
		t.Fatalf("落库规则 = %q, 期望归一为 *.ok.com", rule.Pattern)
	}
	if code := fetchStatus(t, ts, imgFull, "https://evil.io/steal"); code != http.StatusForbidden {
		t.Fatalf("加规则后盗链状态码 = %d, 期望 403", code)
	}
	if code := fetchStatus(t, ts, imgFull, "https://a.ok.com/post"); code != http.StatusOK {
		t.Fatalf("加规则后白名单状态码 = %d, 期望 200", code)
	}

	// 重复添加 → 409;非法规则 → 400。
	dup := adminReq(t, client, csrf, http.MethodPost, ts.URL+"/api/admin/referer-whitelist",
		`{"pattern":"*.ok.com"}`)
	defer closeBody(t, dup)
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("重复添加状态码 = %d, 期望 409", dup.StatusCode)
	}
	bad := adminReq(t, client, csrf, http.MethodPost, ts.URL+"/api/admin/referer-whitelist",
		`{"pattern":"https://x.com"}`)
	defer closeBody(t, bad)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法规则状态码 = %d, 期望 400", bad.StatusCode)
	}

	// 列表可见。
	list := adminReq(t, client, csrf, http.MethodGet, ts.URL+"/api/admin/referer-whitelist", "")
	defer closeBody(t, list)
	var lr refererListResp
	if err := json.NewDecoder(list.Body).Decode(&lr); err != nil {
		t.Fatalf("解析规则列表: %v", err)
	}
	if lr.Total != 1 || len(lr.Items) != 1 || lr.Items[0].Pattern != "*.ok.com" {
		t.Fatalf("规则列表 = %+v, 期望仅 *.ok.com", lr)
	}

	// "重启":以同一个库重建服务,规则从 DB 装载、依然生效。
	restarted, err := referer.New(t.Context(), env.Queries, nil)
	if err != nil {
		t.Fatalf("重建防盗链服务: %v", err)
	}
	if got := restarted.Patterns(); len(got) != 1 || got[0] != "*.ok.com" {
		t.Fatalf("重启后规则 = %v, 期望 [*.ok.com]", got)
	}

	// 删规则 → 即时放行;再删 → 404。
	del := adminReq(t, client, csrf, http.MethodDelete,
		fmt.Sprintf("%s/api/admin/referer-whitelist/%d", ts.URL, rule.ID), "")
	defer closeBody(t, del)
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("删除规则状态码 = %d, 期望 204", del.StatusCode)
	}
	if code := fetchStatus(t, ts, imgFull, "https://evil.io/steal"); code != http.StatusOK {
		t.Fatalf("删规则后状态码 = %d, 期望 200", code)
	}
	again := adminReq(t, client, csrf, http.MethodDelete,
		fmt.Sprintf("%s/api/admin/referer-whitelist/%d", ts.URL, rule.ID), "")
	defer closeBody(t, again)
	if again.StatusCode != http.StatusNotFound {
		t.Fatalf("重复删除状态码 = %d, 期望 404", again.StatusCode)
	}

	// 增删均已落审计。
	for _, action := range []string{"referer.add", "referer.remove"} {
		audit := adminReq(t, client, csrf, http.MethodGet,
			ts.URL+"/api/admin/audit?action="+action+"&target=*.ok.com", "")
		var ar struct {
			Total int64 `json:"total"`
		}
		if err := json.NewDecoder(audit.Body).Decode(&ar); err != nil {
			t.Fatalf("解析审计列表: %v", err)
		}
		closeBody(t, audit)
		if ar.Total != 1 {
			t.Fatalf("审计 %s 条数 = %d, 期望 1", action, ar.Total)
		}
	}
}

// TestConfigFullAdminOnly 验证全量配置切面:管理员可读且分组齐全、无敏感键;
// 普通用户 403。
func TestConfigFullAdminOnly(t *testing.T) {
	var authSvc *auth.Service
	ts := newTestServer(t, func(tc *testConfig, env testEnv) {
		authSvc = tc.Router.Auth
		tc.Admin.Full = adminapi.FullConfigViewFrom(config.Config{
			Listen:     ":8721",
			AccessMode: "proxy",
		})
		if err := env.Queries.CreateUser(t.Context(), sqlc.CreateUserParams{
			ID: "u-viewer", Name: "carol", Role: "user",
		}); err != nil {
			t.Fatalf("建普通用户: %v", err)
		}
	})
	if err := authSvc.SetPassword(t.Context(), "u-viewer", "carol-pass"); err != nil {
		t.Fatalf("设置普通用户密码: %v", err)
	}

	client, csrf := adminSession(t, ts)
	resp := adminReq(t, client, csrf, http.MethodGet, ts.URL+"/api/admin/config/full", "")
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("管理员读取状态码 = %d, 期望 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取全量配置: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析全量配置: %v", err)
	}
	for _, group := range []string{"server", "upload", "lifecycle", "processing", "security", "reliability"} {
		if _, ok := body[group]; !ok {
			t.Errorf("全量配置缺少分组 %q: %s", group, raw)
		}
	}
	for _, sensitive := range []string{"master_key", "password", "token", "secret"} {
		if strings.Contains(strings.ToLower(string(raw)), sensitive) {
			t.Errorf("全量配置疑似泄露敏感键 %q: %s", sensitive, raw)
		}
	}

	viewer, viewerCSRF := loginAs(t, ts, "carol", "carol-pass")
	forbidden := adminReq(t, viewer, viewerCSRF, http.MethodGet, ts.URL+"/api/admin/config/full", "")
	defer closeBody(t, forbidden)
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("普通用户读取状态码 = %d, 期望 403", forbidden.StatusCode)
	}
}
