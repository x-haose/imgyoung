package server

import "testing"

// TestMatchRefererHost 表驱动验证白名单匹配语义:
// 精确域名逐字匹配;"*.example.com" 仅匹配子域,不匹配裸域与后缀伪装。
func TestMatchRefererHost(t *testing.T) {
	patterns := []string{"*.example.com", "blog.foo.dev"}
	cases := map[string]struct {
		host string
		want bool
	}{
		"精确命中":     {"blog.foo.dev", true},
		"子域命中":     {"a.example.com", true},
		"多级子域命中":   {"a.b.example.com", true},
		"裸域不匹配通配":  {"example.com", false},
		"后缀伪装域":    {"evilexample.com", false},
		"伪装子串":     {"example.com.evil.io", false},
		"未列入域":     {"other.dev", false},
		"大小写不敏感":   {"Blog.Foo.DEV", true},
		"通配大小写不敏感": {"A.EXAMPLE.com", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := matchRefererHost(patterns, tc.host); got != tc.want {
				t.Fatalf("matchRefererHost(%q) = %v, 期望 %v", tc.host, got, tc.want)
			}
		})
	}
}

// TestMatchRefererHostEmptyPatterns 验证空白名单不匹配任何主机(启用性由中间件判断)。
func TestMatchRefererHostEmptyPatterns(t *testing.T) {
	if matchRefererHost(nil, "a.example.com") {
		t.Fatal("空白名单不应匹配任何主机")
	}
}
