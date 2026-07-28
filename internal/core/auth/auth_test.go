package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// newTestService 建一个迁移到最新 schema 的临时库,并注册一个已设密码的测试用户。
func newTestService(t *testing.T) (*auth.Service, *sqlc.Queries, *sql.DB) {
	t.Helper()
	conn, queries := dbtest.New(t)
	svc := auth.New(auth.Config{Conn: conn, Queries: queries, Logger: slog.New(slog.DiscardHandler)})
	phc, err := auth.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("哈希密码: %v", err)
	}
	if err := queries.CreateUser(t.Context(), sqlc.CreateUserParams{
		ID: "u1", Name: "alice", PasswordHash: phc, Role: "user",
	}); err != nil {
		t.Fatalf("建测试用户: %v", err)
	}
	return svc, queries, conn
}

// TestLoginAndSession 登录成功产出可校验的会话,登出后会话失效。
func TestLoginAndSession(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := t.Context()

	res, err := svc.Login(ctx, "alice", "correct-horse", "1.1.1.1")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if res.Principal.UserID != "u1" || res.Principal.Role != "user" || res.Principal.IsAdmin() {
		t.Fatalf("主体异常: %+v", res.Principal)
	}
	info, err := svc.ValidateSession(ctx, res.SessionID)
	if err != nil {
		t.Fatalf("会话校验失败: %v", err)
	}
	if info.CSRFToken != res.CSRFToken || info.Principal.Name != "alice" {
		t.Fatalf("会话信息异常: %+v", info)
	}
	if err := svc.Logout(ctx, res.SessionID); err != nil {
		t.Fatalf("登出失败: %v", err)
	}
	if _, err := svc.ValidateSession(ctx, res.SessionID); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("登出后会话应失效, got %v", err)
	}
}

// TestLoginRejections 错误密码/未知用户/未设密码/禁用用户逐一拒绝。
func TestLoginRejections(t *testing.T) {
	svc, queries, _ := newTestService(t)
	ctx := t.Context()

	if _, err := svc.Login(ctx, "alice", "wrong", "2.2.2.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("错误密码: got %v", err)
	}
	if _, err := svc.Login(ctx, "nobody", "x", "2.2.2.2"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("未知用户: got %v", err)
	}
	if err := queries.CreateUser(ctx, sqlc.CreateUserParams{ID: "u2", Name: "bob", Role: "user"}); err != nil {
		t.Fatalf("建未设密码用户: %v", err)
	}
	if _, err := svc.Login(ctx, "bob", "", "2.2.2.3"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("未设密码(空密码)不应可登录: got %v", err)
	}
	if err := queries.UpdateUser(ctx, sqlc.UpdateUserParams{
		Name: "alice", Role: "user", Disabled: 1, ID: "u1",
	}); err != nil {
		t.Fatalf("禁用用户: %v", err)
	}
	if _, err := svc.Login(ctx, "alice", "correct-horse", "2.2.2.4"); !errors.Is(err, auth.ErrUserDisabled) {
		t.Errorf("禁用用户: got %v", err)
	}
}

// TestSessionExpiry 到期会话失效,清理任务能回收到期行。
func TestSessionExpiry(t *testing.T) {
	svc, queries, _ := newTestService(t)
	ctx := t.Context()
	res, err := svc.Login(ctx, "alice", "correct-horse", "3.3.3.3")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	// 直接把会话推到过期,验证校验与回收两条路径。
	if _, err := queries.DeleteExpiredSessions(ctx, sqlc.TimeText(time.Now().Add(31*24*time.Hour))); err != nil {
		t.Fatalf("回收: %v", err)
	}
	if _, err := svc.ValidateSession(ctx, res.SessionID); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("过期会话应失效, got %v", err)
	}
	if n, err := svc.DeleteExpiredSessions(ctx); err != nil || n != 0 {
		t.Fatalf("再次回收应无待清理行: n=%d err=%v", n, err)
	}
}

// TestSetPasswordInvalidatesSessions 改密后既有会话全部失效,新密码可登录。
func TestSetPasswordInvalidatesSessions(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := t.Context()
	res, err := svc.Login(ctx, "alice", "correct-horse", "4.4.4.4")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if err := svc.SetPassword(ctx, "u1", "new-pass"); err != nil {
		t.Fatalf("改密失败: %v", err)
	}
	if _, err := svc.ValidateSession(ctx, res.SessionID); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("改密后旧会话应失效, got %v", err)
	}
	if _, err := svc.Login(ctx, "alice", "new-pass", "4.4.4.5"); err != nil {
		t.Fatalf("新密码应可登录: %v", err)
	}
}

// TestTokenLifecycle 签发→认证→吊销→拒绝的完整链路,含同名约束与使用回写。
func TestTokenLifecycle(t *testing.T) {
	svc, queries, _ := newTestService(t)
	ctx := t.Context()

	issued, err := svc.IssueToken(ctx, "u1", "picgo", 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if len(issued.Plaintext) < 40 || issued.Prefix != issued.Plaintext[:12] {
		t.Fatalf("Token 形态异常: %+v", issued)
	}
	if _, err := svc.IssueToken(ctx, "u1", "picgo", 0); !errors.Is(err, auth.ErrTokenNameTaken) {
		t.Fatalf("同名有效 Token 应冲突, got %v", err)
	}

	tp, err := svc.AuthenticateToken(ctx, issued.Plaintext)
	if err != nil || tp.UserID != "u1" || tp.TokenID != issued.ID {
		t.Fatalf("认证失败: %+v err=%v", tp, err)
	}
	svc.FlushTokenUsage(ctx)
	row, err := queries.GetToken(ctx, issued.ID)
	if err != nil {
		t.Fatalf("查询 Token: %v", err)
	}
	if !row.LastUsedAt.Valid {
		t.Fatalf("使用时刻应已回写: %+v", row.LastUsedAt)
	}

	if err := svc.RevokeToken(ctx, "u1", issued.ID); err != nil {
		t.Fatalf("吊销失败: %v", err)
	}
	if _, err := svc.AuthenticateToken(ctx, issued.Plaintext); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("吊销后应拒绝, got %v", err)
	}
	if err := svc.RevokeToken(ctx, "u1", issued.ID); !errors.Is(err, auth.ErrTokenNotFound) {
		t.Fatalf("重复吊销应不存在, got %v", err)
	}
	if _, err := svc.IssueToken(ctx, "u1", "picgo", 0); err != nil {
		t.Fatalf("吊销后同名应可重签: %v", err)
	}
	if _, err := svc.AuthenticateToken(ctx, "imgy_bogus"); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("未知 Token 应拒绝, got %v", err)
	}
}

// TestRevokeTokenOwnership 吊销带归属校验,他人无法吊销我的 Token。
func TestRevokeTokenOwnership(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := t.Context()
	issued, err := svc.IssueToken(ctx, "u1", "mine", 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if err := svc.RevokeToken(ctx, "attacker", issued.ID); !errors.Is(err, auth.ErrTokenNotFound) {
		t.Fatalf("跨用户吊销应不可见, got %v", err)
	}
	if _, err := svc.AuthenticateToken(context.Background(), issued.Plaintext); err != nil {
		t.Fatalf("Token 应仍有效: %v", err)
	}
}
