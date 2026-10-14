package httpx

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

// TestRequestBaseURL 验证站点根地址推导:经信任代理转发时协议取来源判定、域名优先转发头
// (多级取首值);未转发时回退 Host 与 TLS 判定,转发头一概不看。
func TestRequestBaseURL(t *testing.T) {
	cases := []struct {
		name   string
		target string            // httptest.NewRequest 目标(决定 Host 与 TLS)
		origin *Origin           // 中间件放入的来源判定;nil 表示未经中间件
		hdr    map[string]string // 请求头
		want   string
	}{
		{"裸 HTTP 回退 Host", "http://img.local:8721/api/upload", nil, nil, "http://img.local:8721"},
		{"TLS 连接推 https", "https://img.local/api/upload", nil, nil, "https://img.local"},
		{"未转发时无视转发头", "http://img.local/api/upload", nil, map[string]string{
			"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.example.com",
		}, "http://img.local"},
		{"转发头优先", "http://127.0.0.1:8721/api/upload", &Origin{Secure: true, Forwarded: true}, map[string]string{
			"X-Forwarded-Host": "img.example.com",
		}, "https://img.example.com"},
		{"多级代理取首值", "http://127.0.0.1:8721/api/upload", &Origin{Secure: true, Forwarded: true}, map[string]string{
			"X-Forwarded-Host": "img.example.com, inner.lb",
		}, "https://img.example.com"},
		{"只有转发协议时 Host 仍取请求", "http://img.local/api/upload", &Origin{Secure: true, Forwarded: true}, nil,
			"https://img.local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.target, nil)
			for k, v := range tc.hdr {
				r.Header.Set(k, v)
			}
			if tc.origin != nil {
				r = r.WithContext(WithOrigin(r.Context(), *tc.origin))
			}
			if got := RequestBaseURL(r); got != tc.want {
				t.Errorf("RequestBaseURL = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

// TestClientIPFallsBackToPeer 验证未经中间件时客户端地址取连接对端,IPv4 映射形式还原为 IPv4。
func TestClientIPFallsBackToPeer(t *testing.T) {
	r := httptest.NewRequest("GET", "http://img.local/", nil)
	r.RemoteAddr = "[::ffff:203.0.113.9]:4567"
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, 期望 203.0.113.9", got)
	}
	r.RemoteAddr = "pipe"
	if got := ClientIP(r); got != "pipe" {
		t.Errorf("无法解析时 ClientIP = %q, 期望原样退回 pipe", got)
	}
	if ip := PeerAddr(r); ip != (netip.Addr{}) {
		t.Errorf("无法解析时 PeerAddr = %v, 期望零值", ip)
	}
}
