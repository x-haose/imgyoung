package middleware

import (
	"net/netip"
	"testing"
)

// TestClientFromForwardedFor 验证转发链解析:自右向左跳过信任代理取第一个外部地址,
// 多个头与逗号多跳合并,无法解析的项跳过,全链可信时取最左侧,无可用地址退回对端。
func TestClientFromForwardedFor(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("127.0.0.0/8")}
	peer := netip.MustParseAddr("10.0.0.2")
	for _, tt := range []struct {
		name   string
		values []string
		want   string
	}{
		{"单跳外部客户端", []string{"203.0.113.9"}, "203.0.113.9"},
		{"外部客户端经两级信任代理", []string{"203.0.113.9, 10.0.0.1"}, "203.0.113.9"},
		{"伪造前缀被真实一跳盖过", []string{"1.1.1.1, 203.0.113.9"}, "203.0.113.9"},
		{"多个头合并", []string{"203.0.113.9", "10.0.0.1"}, "203.0.113.9"},
		{"无法解析的项跳过", []string{"garbage, 203.0.113.9, 10.0.0.1"}, "203.0.113.9"},
		{"IPv4 映射地址还原", []string{"::ffff:203.0.113.9"}, "203.0.113.9"},
		{"全链可信取最左", []string{"10.1.1.1, 10.0.0.1"}, "10.1.1.1"},
		{"头缺失退回对端", nil, "10.0.0.2"},
		{"全部无法解析退回对端", []string{"nope"}, "10.0.0.2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientFromForwardedFor(tt.values, trusted, peer); got.String() != tt.want {
				t.Errorf("clientFromForwardedFor = %s, 期望 %s", got, tt.want)
			}
		})
	}
}
