package auth_test

import (
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// forceExpire 把 Token 的到期时刻直接改写到过去,模拟已过期状态
// (正常签发路径无法产出过去的到期时刻)。
func forceExpire(t *testing.T, conn *sql.DB, tokenID int64) {
	t.Helper()
	if _, err := conn.Exec("UPDATE tokens SET expires_at = ? WHERE id = ?",
		sqlc.TimeText(time.Now().Add(-time.Hour)), tokenID); err != nil {
		t.Fatalf("改写到期时刻: %v", err)
	}
}

// TestTokenExpiry 过期校验:带时限的 Token 到期前可认证、到期后与吊销同等拒绝,
// 永久 Token 不受影响。
func TestTokenExpiry(t *testing.T) {
	svc, queries, conn := newTestService(t)
	ctx := t.Context()

	forever, err := svc.IssueToken(ctx, "u1", "forever", 0)
	if err != nil {
		t.Fatalf("签发永久 Token: %v", err)
	}
	timed, err := svc.IssueToken(ctx, "u1", "timed", time.Hour)
	if err != nil {
		t.Fatalf("签发限时 Token: %v", err)
	}
	row, err := queries.GetToken(ctx, timed.ID)
	if err != nil || !row.ExpiresAt.Valid {
		t.Fatalf("限时 Token 应落库到期时刻: %+v err=%v", row.ExpiresAt, err)
	}
	if _, err := svc.AuthenticateToken(ctx, timed.Plaintext); err != nil {
		t.Fatalf("到期前应可认证: %v", err)
	}

	forceExpire(t, conn, timed.ID)
	if _, err := svc.AuthenticateToken(ctx, timed.Plaintext); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("过期后应拒绝, got %v", err)
	}
	if _, err := svc.AuthenticateToken(ctx, forever.Plaintext); err != nil {
		t.Fatalf("永久 Token 应不受影响: %v", err)
	}
}

// TestRotateToken 轮转:同名新签发 + 旧吊销原子完成,不触发同名唯一约束;
// 过期语义继承,失效与跨用户目标一律拒绝。
func TestRotateToken(t *testing.T) {
	svc, queries, _ := newTestService(t)
	ctx := t.Context()

	t.Run("永久Token轮转保持永久", func(t *testing.T) {
		old, err := svc.IssueToken(ctx, "u1", "cli", 0)
		if err != nil {
			t.Fatalf("签发: %v", err)
		}
		issued, err := svc.RotateToken(ctx, "u1", old.ID)
		if err != nil {
			t.Fatalf("轮转失败(同名不应触发唯一约束): %v", err)
		}
		if issued.Name != "cli" || issued.ID == old.ID {
			t.Fatalf("轮转产物异常: %+v", issued)
		}
		if _, err := svc.AuthenticateToken(ctx, issued.Plaintext); err != nil {
			t.Fatalf("新 Token 应可认证: %v", err)
		}
		if _, err := svc.AuthenticateToken(ctx, old.Plaintext); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("旧 Token 应立即失效, got %v", err)
		}
		row, err := queries.GetToken(ctx, issued.ID)
		if err != nil || row.ExpiresAt.Valid {
			t.Fatalf("永久 Token 轮转后应保持永久: %+v err=%v", row.ExpiresAt, err)
		}
	})

	t.Run("限时Token继承原时长从现在起算", func(t *testing.T) {
		old, err := svc.IssueToken(ctx, "u1", "job", 48*time.Hour)
		if err != nil {
			t.Fatalf("签发: %v", err)
		}
		issued, err := svc.RotateToken(ctx, "u1", old.ID)
		if err != nil {
			t.Fatalf("轮转失败: %v", err)
		}
		row, err := queries.GetToken(ctx, issued.ID)
		if err != nil || !row.ExpiresAt.Valid {
			t.Fatalf("轮转产物应带到期时刻: %+v err=%v", row.ExpiresAt, err)
		}
		expires, err := sqlc.ParseTimeText(row.ExpiresAt.String)
		if err != nil {
			t.Fatalf("解析到期时刻: %v", err)
		}
		want := time.Now().Add(48 * time.Hour)
		if diff := expires.Sub(want); diff < -time.Minute || diff > time.Minute {
			t.Fatalf("到期时刻应约为 now+48h: got %v, want %v", expires, want)
		}
	})

	t.Run("失效与跨用户目标拒绝", func(t *testing.T) {
		old, err := svc.IssueToken(ctx, "u1", "gone", 0)
		if err != nil {
			t.Fatalf("签发: %v", err)
		}
		if err := svc.RevokeToken(ctx, "u1", old.ID); err != nil {
			t.Fatalf("吊销: %v", err)
		}
		if _, err := svc.RotateToken(ctx, "u1", old.ID); !errors.Is(err, auth.ErrTokenNotFound) {
			t.Errorf("已吊销 Token 轮转应拒绝, got %v", err)
		}
		mine, err := svc.IssueToken(ctx, "u1", "mine-rotate", 0)
		if err != nil {
			t.Fatalf("签发: %v", err)
		}
		if _, err := svc.RotateToken(ctx, "attacker", mine.ID); !errors.Is(err, auth.ErrTokenNotFound) {
			t.Errorf("跨用户轮转应不可见, got %v", err)
		}
	})
}

// TestDeleteTokenRecord 删除守卫:活跃 Token 拒删,已吊销/已过期可删,跨用户不可见。
func TestDeleteTokenRecord(t *testing.T) {
	svc, queries, conn := newTestService(t)
	ctx := t.Context()

	active, err := svc.IssueToken(ctx, "u1", "active", 0)
	if err != nil {
		t.Fatalf("签发: %v", err)
	}
	if err := svc.DeleteTokenRecord(ctx, "u1", active.ID); !errors.Is(err, auth.ErrTokenActive) {
		t.Fatalf("活跃 Token 应拒删, got %v", err)
	}
	if err := svc.DeleteTokenRecord(ctx, "attacker", active.ID); !errors.Is(err, auth.ErrTokenNotFound) {
		t.Fatalf("跨用户删除应不可见, got %v", err)
	}

	if err := svc.RevokeToken(ctx, "u1", active.ID); err != nil {
		t.Fatalf("吊销: %v", err)
	}
	if err := svc.DeleteTokenRecord(ctx, "u1", active.ID); err != nil {
		t.Fatalf("已吊销记录应可删: %v", err)
	}
	if _, err := queries.GetToken(ctx, active.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后行应不存在, got %v", err)
	}

	expired, err := svc.IssueToken(ctx, "u1", "expired", time.Hour)
	if err != nil {
		t.Fatalf("签发: %v", err)
	}
	forceExpire(t, conn, expired.ID)
	if err := svc.DeleteTokenRecord(ctx, "u1", expired.ID); err != nil {
		t.Fatalf("已过期记录应可删: %v", err)
	}
}

// TestDeleteTokenRecordNullsImageSource 删除失效 Token 时,其上传过的图片保留、
// 来源置空(外键 ON DELETE SET NULL),存量图片不阻塞删除。
func TestDeleteTokenRecordNullsImageSource(t *testing.T) {
	svc, queries, conn := newTestService(t)
	ctx := t.Context()

	tok, err := svc.IssueToken(ctx, "u1", "uploader", 0)
	if err != nil {
		t.Fatalf("签发: %v", err)
	}
	blobID, err := queries.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "s", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := queries.CreateImage(ctx, sqlc.CreateImageParams{
		ID: "imgbytoken", BlobID: blobID, UserID: "u1",
		TokenID: sql.NullInt64{Int64: tok.ID, Valid: true},
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}

	if err := svc.RevokeToken(ctx, "u1", tok.ID); err != nil {
		t.Fatalf("吊销: %v", err)
	}
	if err := svc.DeleteTokenRecord(ctx, "u1", tok.ID); err != nil {
		t.Fatalf("有存量图片的失效 Token 应可删: %v", err)
	}
	var tid sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		"SELECT token_id FROM images WHERE id = 'imgbytoken'").Scan(&tid); err != nil {
		t.Fatalf("图片应保留: %v", err)
	}
	if tid.Valid {
		t.Errorf("token_id 应被置空, got %v", tid.Int64)
	}
}

// TestBootstrapGuard bootstrap 守卫:环境变量托管时吊销/轮转/删除一律拒绝,
// 未托管时按普通 Token 处理。
func TestBootstrapGuard(t *testing.T) {
	svc, queries, conn := newTestService(t)
	ctx := t.Context()
	managed := auth.New(auth.Config{
		Conn: conn, Queries: queries, BootstrapManaged: true,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := managed.EnsureBootstrapToken(ctx, sqlc.DefaultUserID, "boot-secret"); err != nil {
		t.Fatalf("植入引导 Token: %v", err)
	}
	rows, err := queries.ListTokensByUser(ctx, sqlc.ListTokensByUserParams{
		UserID: sqlc.DefaultUserID, Limit: 10, Offset: 0,
	})
	if err != nil || len(rows) != 1 || rows[0].Name != "bootstrap" {
		t.Fatalf("引导 Token 应在库: rows=%+v err=%v", rows, err)
	}
	bootID := rows[0].ID

	if !managed.IsBootstrapToken(rows[0]) {
		t.Error("托管中的引导 Token 应被识别")
	}
	other := rows[0]
	other.UserID = "u1"
	if managed.IsBootstrapToken(other) {
		t.Error("他人同名 Token 不应视为引导 Token")
	}
	if svc.IsBootstrapToken(rows[0]) {
		t.Error("未配置环境变量时不应视为托管")
	}
	revoked := rows[0]
	revoked.RevokedAt = sqlc.TimeNull(time.Now())
	if managed.IsBootstrapToken(revoked) {
		t.Error("已吊销的引导历史行不应视为托管")
	}

	if err := managed.RevokeToken(ctx, sqlc.DefaultUserID, bootID); !errors.Is(err, auth.ErrTokenProtected) {
		t.Errorf("托管引导 Token 吊销应拒绝, got %v", err)
	}
	if _, err := managed.RotateToken(ctx, sqlc.DefaultUserID, bootID); !errors.Is(err, auth.ErrTokenProtected) {
		t.Errorf("托管引导 Token 轮转应拒绝, got %v", err)
	}
	if err := managed.DeleteTokenRecord(ctx, sqlc.DefaultUserID, bootID); !errors.Is(err, auth.ErrTokenProtected) {
		t.Errorf("托管引导 Token 删除应拒绝, got %v", err)
	}

	// 未托管(环境变量已移除)时按普通 Token 处理:可正常吊销。
	if err := svc.RevokeToken(ctx, sqlc.DefaultUserID, bootID); err != nil {
		t.Errorf("未托管时应可吊销, got %v", err)
	}
}

// TestBootstrapHistoryDeletable 环境变量换值后:旧引导行被吊销为历史行,
// 托管守卫只护当前有效行,历史行可正常删除。
func TestBootstrapHistoryDeletable(t *testing.T) {
	_, queries, conn := newTestService(t)
	ctx := t.Context()
	managed := auth.New(auth.Config{
		Conn: conn, Queries: queries, BootstrapManaged: true,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := managed.EnsureBootstrapToken(ctx, sqlc.DefaultUserID, "boot-v1"); err != nil {
		t.Fatalf("植入引导 Token: %v", err)
	}
	if err := managed.EnsureBootstrapToken(ctx, sqlc.DefaultUserID, "boot-v2"); err != nil {
		t.Fatalf("换值重建引导 Token: %v", err)
	}
	rows, err := queries.ListTokensByUser(ctx, sqlc.ListTokensByUserParams{
		UserID: sqlc.DefaultUserID, Limit: 10, Offset: 0,
	})
	if err != nil || len(rows) != 2 {
		t.Fatalf("换值后应有新旧两行: rows=%+v err=%v", rows, err)
	}
	var oldID, newID int64
	for _, row := range rows {
		if row.RevokedAt.Valid {
			oldID = row.ID
		} else {
			newID = row.ID
		}
	}
	if oldID == 0 || newID == 0 {
		t.Fatalf("应恰有一条已吊销与一条有效: rows=%+v", rows)
	}
	if err := managed.DeleteTokenRecord(ctx, sqlc.DefaultUserID, oldID); err != nil {
		t.Errorf("已吊销的引导历史行应可删, got %v", err)
	}
	if err := managed.DeleteTokenRecord(ctx, sqlc.DefaultUserID, newID); !errors.Is(err, auth.ErrTokenProtected) {
		t.Errorf("当前托管行删除应拒绝, got %v", err)
	}
}

// TestIssueTokenReclaimsExpiredName 同名占用回收:同名旧 Token 已过期时
// 重新签发应自动吊销过期行并成功;同名 Token 仍有效时保持重名冲突。
func TestIssueTokenReclaimsExpiredName(t *testing.T) {
	svc, queries, conn := newTestService(t)
	ctx := t.Context()

	old, err := svc.IssueToken(ctx, "u1", "picgo", time.Hour)
	if err != nil {
		t.Fatalf("签发: %v", err)
	}
	if _, err := svc.IssueToken(ctx, "u1", "picgo", time.Hour); !errors.Is(err, auth.ErrTokenNameTaken) {
		t.Fatalf("同名有效 Token 应冲突, got %v", err)
	}

	forceExpire(t, conn, old.ID)
	reissued, err := svc.IssueToken(ctx, "u1", "picgo", time.Hour)
	if err != nil {
		t.Fatalf("同名旧 Token 过期后应可重新签发: %v", err)
	}
	if _, err := svc.AuthenticateToken(ctx, reissued.Plaintext); err != nil {
		t.Fatalf("新 Token 应可认证: %v", err)
	}
	row, err := queries.GetToken(ctx, old.ID)
	if err != nil || !row.RevokedAt.Valid {
		t.Fatalf("过期旧行应被吊销: %+v err=%v", row.RevokedAt, err)
	}
}
