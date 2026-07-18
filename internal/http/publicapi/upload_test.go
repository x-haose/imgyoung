package publicapi

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestResolveTTL 验证 ttl 字段解析:never/0 显式永久、空值视为未显式指定
// (回退交上传服务按用户/全局默认解析),非整数、负数与超上限
// (含会导致 time.Duration 溢出的巨值)一律判非法。
func TestResolveTTL(t *testing.T) {
	h := New(Config{})
	cases := []struct {
		name     string
		raw      string
		want     time.Duration
		explicit bool
		ok       bool
	}{
		{"never 显式永久", "never", 0, true, true},
		{"空值未显式指定", "", 0, false, true},
		{"0 显式永久", "0", 0, true, true},
		{"合法秒数", "3600", time.Hour, true, true},
		{"上限恰好可用", strconv.Itoa(maxTTLSeconds), time.Duration(maxTTLSeconds) * time.Second, true, true},
		{"超上限拒绝", strconv.Itoa(maxTTLSeconds + 1), 0, false, false},
		{"溢出巨值拒绝", "10000000000000000000", 0, false, false},
		{"负数拒绝", "-1", 0, false, false},
		{"非整数拒绝", "abc", 0, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.raw != "" {
				form.Set("ttl", tc.raw)
			}
			req := httptest.NewRequest("POST", "/api/upload", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			got, explicit, ok := h.resolveTTL(req)
			if got != tc.want || explicit != tc.explicit || ok != tc.ok {
				t.Errorf("resolveTTL(%q) = (%v, %t, %t), 期望 (%v, %t, %t)",
					tc.raw, got, explicit, ok, tc.want, tc.explicit, tc.ok)
			}
		})
	}
}
