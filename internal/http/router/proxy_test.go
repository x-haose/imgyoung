package router_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/auth"
)

// TestUploadURLDerivedFromRequest 验证未配置 BaseURL 时按请求推导绝对地址:
// 连接对端是信任的反代(默认回环)时转发头优先于连接本身;不信任时转发头一概不看。
func TestUploadURLDerivedFromRequest(t *testing.T) {
	forwarded := map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "pub.example.com",
	}
	ts := newTestServer(t, nil)
	resp := uploadReq(t, ts, "a.png", pngBytes(t), forwarded)
	defer closeBody(t, resp)
	if got := rawUploadURL(t, resp); !strings.HasPrefix(got, "https://pub.example.com/i/") {
		t.Fatalf("url = %q, 期望按转发头推导的绝对地址", got)
	}

	untrusting := newTestServer(t, func(tc *testConfig, _ testEnv) { tc.Router.TrustedProxies = nil })
	resp2 := uploadReq(t, untrusting, "a.png", pngBytes(t), forwarded)
	defer closeBody(t, resp2)
	if got := rawUploadURL(t, resp2); !strings.HasPrefix(got, untrusting.URL+"/i/") {
		t.Fatalf("url = %q, 期望不信任反代时按连接本身推导(%s)", got, untrusting.URL)
	}
}

// TestLoginRateLimitKeyedByForwardedClient 验证登录限速的地址键取转发链里的真实客户端:
// 同一反代后面的不同客户端各自计数,一个客户端爆破不会把别人一起限掉。
// 两个客户端用不同用户名,排除用户名键的干扰;若地址键取的是反代地址,第二个客户端必然 429。
func TestLoginRateLimitKeyedByForwardedClient(t *testing.T) {
	ts := newTestServer(t, nil)
	post := func(client, user string) int {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/admin/auth/login",
			strings.NewReader(fmt.Sprintf(`{"name":%q,"password":"wrong-password"}`, user)))
		if err != nil {
			t.Fatalf("构造登录请求: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", client)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("发送登录请求: %v", err)
		}
		closeBody(t, resp)
		return resp.StatusCode
	}
	for range auth.LoginBurst {
		if code := post("203.0.113.9", "nobody-a"); code != http.StatusUnauthorized {
			t.Fatalf("爆破期间状态码 = %d, 期望 401", code)
		}
	}
	if code := post("203.0.113.9", "nobody-a"); code != http.StatusTooManyRequests {
		t.Fatalf("超出突发额度后状态码 = %d, 期望 429", code)
	}
	if code := post("198.51.100.7", "nobody-b"); code != http.StatusUnauthorized {
		t.Fatalf("另一客户端状态码 = %d, 期望 401(不受前者限速牵连)", code)
	}
}
