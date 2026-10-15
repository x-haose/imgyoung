//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// oauthStartResult 发起授权的响应体。
type oauthStartResult struct {
	AuthURL string `json:"auth_url"` // 引导用户前往的授权地址
	State   string `json:"state"`    // 领取结果用的一次性标识
}

// oauthResult 领取授权结果的响应体。
type oauthResult struct {
	Pending bool              `json:"pending"` // 用户尚未完成授权
	Token   string            `json:"token"`   // 授权令牌
	Code    string            `json:"code"`    // 授权失败的错误码
	Params  map[string]string `json:"params"`  // 授权失败的文案参数
}

// oauthStart 对指定模板发起授权,返回状态码与响应体(非 200 时响应体为零值)。
// @param template 模板 id
// @return 状态码
// @return 授权地址与 state
func oauthStart(t *testing.T, inst *instance, client *http.Client, csrf,
	template string) (int, oauthStartResult) {
	t.Helper()
	body := `{"template":"` + template + `","inputs":{"client_id":"cid-e2e","client_secret":"sec-e2e"}}`
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channel-oauth/start", body)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读发起授权响应: %v", err)
	}
	var out oauthStartResult
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("解析发起授权响应: %v; body=%s", err, raw)
		}
	}
	return resp.StatusCode, out
}

// oauthTake 领取一次授权结果,返回状态码与响应体(非 200 时响应体为零值)。
// @param state 发起授权时得到的标识
// @return 状态码
// @return 授权结果
func oauthTake(t *testing.T, inst *instance, client *http.Client, csrf,
	state string) (int, oauthResult) {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet,
		inst.baseURL+"/api/admin/channel-oauth/result?state="+url.QueryEscape(state), "")
	defer func() { _ = resp.Body.Close() }()
	var out oauthResult
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("解析授权结果: %v", err)
		}
	}
	return resp.StatusCode, out
}

// oauthCallback 以会话访问授权回调页,返回状态码与页面正文。
// @param query 回调查询串(不含问号)
// @return 状态码
// @return 页面正文
func oauthCallback(t *testing.T, inst *instance, client *http.Client, csrf,
	query string) (int, string) {
	t.Helper()
	target := inst.baseURL + "/api/admin/channel-oauth/callback?" + query
	resp := adminReq(t, client, csrf, http.MethodGet, target, "")
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读回调页: %v", err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("回调页 Content-Type = %q, 期望 text/html", ct)
	}
	return resp.StatusCode, string(raw)
}

// TestOAuthStartRequiresBaseURL 未配置对外站点地址时无法发起授权:回调地址无从确定,400 并提示先配置。
func TestOAuthStartRequiresBaseURL(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	if code, _ := oauthStart(t, inst, admin, csrf, "googledrive"); code != http.StatusBadRequest {
		t.Fatalf("未配 base_url 发起授权状态码 = %d, 期望 400", code)
	}
}

// TestOAuthFlowEndpoints 授权三端点的本地闭环(不出网):发起得到带 state 与本站回调地址的授权地址;
// 未完成时结果为等待中;未知 state 的回调 400;用户拒绝授权的回调记下失败,结果领取一次后即失效。
func TestOAuthFlowEndpoints(t *testing.T) {
	t.Parallel()
	const base = "https://img.example.com"
	inst := startInstance(t, map[string]any{"base_url": base})
	admin, csrf := adminSession(t, inst)

	code, started := oauthStart(t, inst, admin, csrf, "googledrive")
	if code != http.StatusOK {
		t.Fatalf("发起授权状态码 = %d, 期望 200", code)
	}
	if started.State == "" || !strings.Contains(started.AuthURL, "state="+started.State) {
		t.Fatalf("授权地址未携带 state: state=%q url=%q", started.State, started.AuthURL)
	}
	wantRedirect := url.QueryEscape(base + "/api/admin/channel-oauth/callback")
	if !strings.Contains(started.AuthURL, wantRedirect) {
		t.Fatalf("授权地址回调参数 = %q, 期望含本站回调地址 %s", started.AuthURL, wantRedirect)
	}
	if !strings.Contains(started.AuthURL, "client_id=cid-e2e") {
		t.Fatalf("授权地址未用自建应用标识: %q", started.AuthURL)
	}

	code, got := oauthTake(t, inst, admin, csrf, started.State)
	if code != http.StatusOK || !got.Pending {
		t.Fatalf("未完成时领取结果: 状态码 %d 结果 %+v, 期望 200 且 pending", code, got)
	}

	code, _ = oauthCallback(t, inst, admin, csrf, "state=no-such-state&code=x")
	if code != http.StatusBadRequest {
		t.Fatalf("未知 state 回调状态码 = %d, 期望 400", code)
	}

	denied := "state=" + url.QueryEscape(started.State) + "&error=access_denied"
	code, page := oauthCallback(t, inst, admin, csrf, denied)
	if code != http.StatusOK || !strings.Contains(page, "授权未完成") {
		t.Fatalf("用户拒绝授权的回调: 状态码 %d 页面 %q, 期望 200 且提示授权未完成", code, page)
	}
	code, got = oauthTake(t, inst, admin, csrf, started.State)
	if code != http.StatusOK || got.Pending || got.Token != "" || got.Code != "oauth_denied" ||
		got.Params["detail"] != "access_denied" {
		t.Fatalf("拒绝后领取结果: 状态码 %d 结果 %+v, 期望 oauth_denied、原因 access_denied 且无令牌", code, got)
	}
	if code, _ := oauthTake(t, inst, admin, csrf, started.State); code != http.StatusNotFound {
		t.Fatalf("结果二次领取状态码 = %d, 期望 404(只可领取一次)", code)
	}

	if code, _ := oauthStart(t, inst, admin, csrf, "aws-s3"); code != http.StatusBadRequest {
		t.Fatalf("对无需授权的模板发起授权状态码 = %d, 期望 400", code)
	}
	missing := adminReq(t, admin, csrf, http.MethodPost, inst.baseURL+"/api/admin/channel-oauth/start",
		`{"template":"googledrive","inputs":{"client_id":"cid-e2e"},"channel_id":99999}`)
	_ = missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("编辑场景指向不存在的渠道发起授权状态码 = %d, 期望 404", missing.StatusCode)
	}
}
