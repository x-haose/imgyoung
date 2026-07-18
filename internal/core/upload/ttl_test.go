package upload

import (
	"database/sql"
	"testing"
	"time"
)

// TestResolveTTLPriority 验证 TTL 三层优先级矩阵:
// 请求显式(含 0=永久)> 用户默认(0=该用户默认永久)> 全局默认。
func TestResolveTTLPriority(t *testing.T) {
	userNull := sql.NullInt64{}
	user5h := sql.NullInt64{Int64: 5, Valid: true}
	user0 := sql.NullInt64{Int64: 0, Valid: true}
	cases := []struct {
		name      string
		opts      Options
		userHours sql.NullInt64
		global    time.Duration
		want      time.Duration
	}{
		{"显式秒数压过用户与全局", Options{TTL: time.Minute, TTLSet: true}, user5h, 24 * time.Hour, time.Minute},
		{"显式 0 即永久压过用户与全局", Options{TTL: 0, TTLSet: true}, user5h, 24 * time.Hour, 0},
		{"未显式时用户默认生效", Options{}, user5h, 24 * time.Hour, 5 * time.Hour},
		{"用户默认 0 即该用户永久压过全局", Options{}, user0, 24 * time.Hour, 0},
		{"用户随全局时取全局默认", Options{}, userNull, 24 * time.Hour, 24 * time.Hour},
		{"三层皆空即永久", Options{}, userNull, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTTL(tc.opts, tc.userHours, tc.global); got != tc.want {
				t.Errorf("resolveTTL = %v, 期望 %v", got, tc.want)
			}
		})
	}
}
