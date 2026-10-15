//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// loginMustChange 发一次登录并返回状态码与响应体的 must_change_password 标记。
func loginMustChange(t *testing.T, baseURL, name, password string) (int, bool) {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"password":%q}`, name, password)
	resp, err := newClient(t).Post(baseURL+"/api/admin/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("发送登录请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, false
	}
	var lr struct {
		User struct {
			MustChangePassword bool `json:"must_change_password"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析登录响应: %v", err)
	}
	return resp.StatusCode, lr.User.MustChangePassword
}

// postLogin 发送一次登录请求并返回状态码。
func postLogin(t *testing.T, client *http.Client, baseURL, name, password string) int {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"password":%q}`, name, password)
	resp, err := client.Post(baseURL+"/api/admin/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("发送登录请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestDefaultAdminPassword 零配置首启:出厂默认密码可直接登录,且响应标记须改密;错误密码 401。
func TestDefaultAdminPassword(t *testing.T) {
	t.Parallel()
	inst := bootInstance(t)

	code, mustChange := loginMustChange(t, inst.baseURL, "admin", defaultAdminPass)
	if code != http.StatusOK {
		t.Fatalf("默认密码登录状态码 = %d, 期望 200", code)
	}
	if !mustChange {
		t.Fatal("默认密码登录应返回 must_change_password=true")
	}
	// 反面:随意密码仍应被拒,确认不是"任意密码放行"。
	code = postLogin(t, newClient(t), inst.baseURL, "admin", "not-the-password")
	if code != http.StatusUnauthorized {
		t.Fatalf("错误密码状态码 = %d, 期望 401", code)
	}
}

// TestFirstRunPasswordChange 强制改密闭环:默认密码改掉后,新密码登录不再须改密,旧默认密码失效。
func TestFirstRunPasswordChange(t *testing.T) {
	t.Parallel()
	inst := bootInstance(t)
	client, csrf := loginSession(t, inst, "admin", defaultAdminPass)

	const newPass = "brand-new-admin-pass"
	resp := adminReq(t, client, csrf, http.MethodPut, inst.baseURL+"/api/admin/me/password",
		fmt.Sprintf(`{"old_password":%q,"new_password":%q}`, defaultAdminPass, newPass))
	code := resp.StatusCode
	_ = resp.Body.Close()
	if code != http.StatusNoContent {
		t.Fatalf("改密状态码 = %d, 期望 204", code)
	}

	loginCode, mustChange := loginMustChange(t, inst.baseURL, "admin", newPass)
	if loginCode != http.StatusOK {
		t.Fatalf("新密码登录状态码 = %d, 期望 200", loginCode)
	}
	if mustChange {
		t.Fatal("改密后 must_change_password 应为 false")
	}
	// 旧默认密码改后即失效。
	if c := postLogin(t, newClient(t), inst.baseURL, "admin", defaultAdminPass); c != http.StatusUnauthorized {
		t.Fatalf("改密后旧默认密码状态码 = %d, 期望 401", c)
	}
}

// TestDefaultPasswordGate 仍为出厂默认密码的会话只能退出、查看自己与改密,其余接口一律 403;改密后放行。
func TestDefaultPasswordGate(t *testing.T) {
	t.Parallel()
	inst := bootInstance(t)
	client, csrf := loginSession(t, inst, "admin", defaultAdminPass)
	status := func(c *http.Client, token, method, path, body string) int {
		t.Helper()
		resp := adminReq(t, c, token, method, inst.baseURL+path, body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/admin/dashboard", ""},
		{http.MethodPost, "/api/admin/tokens", `{"name":"script"}`},
		{http.MethodGet, "/api/admin/channels", ""},
	} {
		if code := status(client, csrf, c.method, c.path, c.body); code != http.StatusForbidden {
			t.Errorf("默认密码会话 %s %s = %d, 期望 403", c.method, c.path, code)
		}
	}
	if code := status(client, csrf, http.MethodGet, "/api/admin/me", ""); code != http.StatusOK {
		t.Fatalf("默认密码会话 GET /me = %d, 期望 200", code)
	}

	retireDefaultPassword(t, inst)
	admin, adminCSRF := adminSession(t, inst)
	if code := status(admin, adminCSRF, http.MethodGet, "/api/admin/dashboard", ""); code != http.StatusOK {
		t.Fatalf("改密后 GET /dashboard = %d, 期望 200", code)
	}
}

// TestAdminAuthLifecycle 管理面认证闭环:登录→/me 恢复会话→登出→会话失效;错误密码 401。
func TestAdminAuthLifecycle(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)

	if code := postLogin(t, newClient(t), inst.baseURL, "admin", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("错误密码状态码 = %d, 期望 401", code)
	}

	client, csrf := adminSession(t, inst)
	me := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/me", "")
	if me.StatusCode != http.StatusOK {
		t.Fatalf("/me 状态码 = %d, 期望 200", me.StatusCode)
	}
	var mr struct {
		User struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(me.Body).Decode(&mr); err != nil {
		t.Fatalf("解析 /me: %v", err)
	}
	_ = me.Body.Close()
	if mr.User.Name != "admin" || mr.User.Role != "admin" || mr.CSRFToken != csrf {
		t.Fatalf("/me 内容异常: %+v", mr)
	}

	lo := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/auth/logout", "")
	loCode := lo.StatusCode
	_ = lo.Body.Close()
	if loCode != http.StatusNoContent {
		t.Fatalf("登出状态码 = %d, 期望 204", loCode)
	}
	after := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/me", "")
	afterCode := after.StatusCode
	_ = after.Body.Close()
	if afterCode != http.StatusUnauthorized {
		t.Fatalf("登出后 /me 状态码 = %d, 期望 401", afterCode)
	}
}

// TestCSRFEnforced 反向断言:持有效会话但缺 CSRF 头的状态变更请求必须 403。
func TestCSRFEnforced(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, _ := adminSession(t, inst)

	req, err := http.NewRequest(http.MethodPost, inst.baseURL+"/api/admin/channels",
		strings.NewReader(`{"name":"x","driver":"local"}`))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("缺 CSRF 状态码 = %d, 期望 403", resp.StatusCode)
	}
}

// TestLoginRateLimited 连续错误密码触发登录限速(突发额度 5 次)。
func TestLoginRateLimited(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client := newSessionClient(t)
	var last int
	for range 8 {
		last = postLogin(t, client, inst.baseURL, "admin", "wrong")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("连续失败后状态码 = %d, 期望 429", last)
	}
}

// TestTokenAuthBoundary Token 鉴权边界:后台签发的 Token 可上传,伪造值 401,
// 管理面拒绝 Token 凭证(Token 只授权数据 API)。
func TestTokenAuthBoundary(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)

	forged := upload(t, inst, "forged-token", "e2e.png", pngBytes(t), nil, nil)
	forgedCode := forged.StatusCode
	_ = forged.Body.Close()
	if forgedCode != http.StatusUnauthorized {
		t.Fatalf("伪造 Token 上传状态码 = %d, 期望 401", forgedCode)
	}
	_, _ = mustUpload(t, inst, pngBytes(t), nil, nil)

	req, err := http.NewRequest(http.MethodGet, inst.baseURL+"/api/admin/channels", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	resp, err := newClient(t).Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Token 凭证访问管理面状态码 = %d, 期望 401", resp.StatusCode)
	}
}
