package publicapi

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestResolveTTL 验证 ttl 字段解析:never/空值/合法秒数各归其位,
// 非整数、负数与超上限(含会导致 time.Duration 溢出的巨值)一律判非法。
func TestResolveTTL(t *testing.T) {
	h := New(Config{DefaultTTL: 30 * time.Minute})
	cases := []struct {
		name string
		raw  string
		want time.Duration
		ok   bool
	}{
		{"never 显式永久", "never", 0, true},
		{"空值走全局默认", "", 30 * time.Minute, true},
		{"0 显式永久", "0", 0, true},
		{"合法秒数", "3600", time.Hour, true},
		{"上限恰好可用", strconv.Itoa(maxTTLSeconds), time.Duration(maxTTLSeconds) * time.Second, true},
		{"超上限拒绝", strconv.Itoa(maxTTLSeconds + 1), 0, false},
		{"溢出巨值拒绝", "10000000000000000000", 0, false},
		{"负数拒绝", "-1", 0, false},
		{"非整数拒绝", "abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.raw != "" {
				form.Set("ttl", tc.raw)
			}
			req := httptest.NewRequest("POST", "/api/upload", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			got, ok := h.resolveTTL(req)
			if got != tc.want || ok != tc.ok {
				t.Errorf("resolveTTL(%q) = (%v, %t), 期望 (%v, %t)", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}
