// Package apitest 提供管理接口测试共用的请求构造:进程内的路由测试与端到端测试都经它以管理员会话发请求。
// 只供测试代码导入。
package apitest

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// AdminRequest 以管理员会话发送管理接口请求:有请求体时按 JSON 发送,非 GET 请求带上 CSRF 令牌头。
// @param t 测试上下文,构造或发送失败即 Fatal
// @param client 持有会话 cookie 的客户端
// @param csrf 登录或 me 接口下发的 CSRF 令牌
// @param method HTTP 方法
// @param url 完整地址
// @param body JSON 请求体;空串表示没有请求体
// @return 响应,调用方负责关闭与断言
func AdminRequest(t *testing.T, client *http.Client, csrf, method, url, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("构造管理请求: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发送管理请求: %v", err)
	}
	return resp
}
