//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

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

// TestAdminAuthLifecycle 管理面认证闭环:登录→/me 恢复会话→登出→会话失效;错误密码 401。
func TestAdminAuthLifecycle(t *testing.T) {
	inst := startInstance(t, nil)

	if code := postLogin(t, http.DefaultClient, inst.baseURL, "admin", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("错误密码状态码 = %d, 期望 401", code)
	}

	client, csrf := adminSession(t, inst)
	me := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/me", "")
	code := me.StatusCode
	_ = me.Body.Close()
	if code != http.StatusOK {
		t.Fatalf("/me 状态码 = %d, 期望 200", code)
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
	inst := startInstance(t, nil)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	var last int
	for range 8 {
		last = postLogin(t, client, inst.baseURL, "admin", "wrong")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("连续失败后状态码 = %d, 期望 429", last)
	}
}

// TestTokenIsSeededIntoDB 引导 Token 语义:环境变量值可上传,伪造值 401,
// 管理面拒绝 Token 凭证(Token 只授权数据 API)。
func TestTokenIsSeededIntoDB(t *testing.T) {
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
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Token 凭证访问管理面状态码 = %d, 期望 401", resp.StatusCode)
	}
}
