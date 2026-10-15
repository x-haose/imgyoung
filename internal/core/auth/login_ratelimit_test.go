package auth

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// TestLoginRateLimitIntegration 同一 IP 连续失败尝试超过突发额度后被限速。
// @Description: 用固定时钟隔断额度恢复,断言只取决于尝试次数而非墙钟。
// 走真实时钟时,每次尝试都要付一遍 argon2id 的代价(竞态检测下更慢),
// 耗时逼近甚至超过额度恢复间隔,早期消耗会被恢复抵消,限速判定随机化。
// 本用例是内部测试,得以注入时钟;额度恢复语义另由 limiter 单元测试覆盖。
func TestLoginRateLimitIntegration(t *testing.T) {
	svc := newFrozenLoginService(t)

	// 额度耗尽后应立即被拒;多留一次余量以暴露"晚一拍才限速"。
	var last error
	for range LoginBurst + 2 {
		_, last = svc.Login(t.Context(), "alice", "wrong", "9.9.9.9")
		if errors.Is(last, ErrTooManyAttempts) {
			return
		}
	}
	t.Fatalf("连续 %d 次失败后仍未限速, got %v", LoginBurst+2, last)
}

// TestLoginRateLimitResetOnSuccess 凭证正确即清空限速计数,合法用户不被失败尝试连累。
func TestLoginRateLimitResetOnSuccess(t *testing.T) {
	svc := newFrozenLoginService(t)

	// 用掉部分额度后成功登录,额度应归零重来。
	for range LoginBurst - 1 {
		if _, err := svc.Login(t.Context(), "alice", "wrong", "9.9.9.9"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("失败尝试应报凭证错误, got %v", err)
		}
	}
	if _, err := svc.Login(t.Context(), "alice", "correct-horse", "9.9.9.9"); err != nil {
		t.Fatalf("正确凭证登录失败: %v", err)
	}
	// 计数已清零,下一次失败不应立刻触顶。
	if _, err := svc.Login(t.Context(), "alice", "wrong", "9.9.9.9"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("成功登录后额度未重置, got %v", err)
	}
}

// newFrozenLoginService 装配连真实库的认证服务:建好用户 alice(密码 correct-horse),限速时钟冻结在当下。
// @return 认证服务
func newFrozenLoginService(t *testing.T) *Service {
	t.Helper()
	conn, queries := dbtest.New(t)
	svc := New(Config{Conn: conn, Queries: queries, Logger: slog.New(slog.DiscardHandler)})
	phc, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("哈希密码: %v", err)
	}
	if err := queries.CreateUser(t.Context(), sqlc.CreateUserParams{
		ID: "u1", Name: "alice", PasswordHash: phc, Role: "user",
	}); err != nil {
		t.Fatalf("建测试用户: %v", err)
	}
	frozen := time.Now()
	svc.limiter.mu.Lock()
	svc.limiter.now = func() time.Time { return frozen }
	svc.limiter.mu.Unlock()
	return svc
}
