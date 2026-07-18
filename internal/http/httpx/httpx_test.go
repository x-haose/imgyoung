package httpx

import (
	"net/http/httptest"
	"testing"
)

// TestRequestBaseURL 验证站点根地址推导:转发头优先(多级取首值),
// 缺头时回退 Host 与 TLS 判定。
func TestRequestBaseURL(t *testing.T) {
	cases := []struct {
		name   string
		target string            // httptest.NewRequest 目标(决定 Host 与 TLS)
		hdr    map[string]string // 请求头
		want   string
	}{
		{"裸 HTTP 回退 Host", "http://img.local:8721/api/upload", nil, "http://img.local:8721"},
		{"TLS 连接推 https", "https://img.local/api/upload", nil, "https://img.local"},
		{"转发头优先", "http://127.0.0.1:8721/api/upload", map[string]string{
			"X-Forwarded-Proto": "https", "X-Forwarded-Host": "img.example.com",
		}, "https://img.example.com"},
		{"多级代理取首值", "http://127.0.0.1:8721/api/upload", map[string]string{
			"X-Forwarded-Proto": "https, http", "X-Forwarded-Host": "img.example.com, inner.lb",
		}, "https://img.example.com"},
		{"只有转发协议时 Host 仍取请求", "http://img.local/api/upload", map[string]string{
			"X-Forwarded-Proto": "https",
		}, "https://img.local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.target, nil)
			for k, v := range tc.hdr {
				r.Header.Set(k, v)
			}
			if got := RequestBaseURL(r); got != tc.want {
				t.Errorf("RequestBaseURL = %q, 期望 %q", got, tc.want)
			}
		})
	}
}
