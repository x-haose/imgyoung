package lifecycle

import (
	"log/slog"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// TestExpireOneSkipsCountWhenRowGone 验证回收事务的原子性:引用行已被并发路径删掉、本次删引用影响 0 行时,
// 既不递减引用计数也不打待删标记——计数已由那条路径处理。
func TestExpireOneSkipsCountWhenRowGone(t *testing.T) {
	conn, q := dbtest.New(t)
	ctx := t.Context()
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID: "ghostimg01", BlobID: blobID, UserID: sqlc.DefaultUserID,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 扫描已拿到这条引用之后、回收之前,另一条路径抢先删掉了引用行。
	if _, err := conn.ExecContext(ctx, "DELETE FROM images WHERE id = 'ghostimg01'"); err != nil {
		t.Fatalf("抢先删引用: %v", err)
	}

	s := New(Config{Conn: conn, Queries: q, Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler)})
	if err := s.expireOne(ctx, "ghostimg01", blobID); err != nil {
		t.Fatalf("expireOne 返回错误: %v", err)
	}
	var refCount, pendingDelete int
	if err := conn.QueryRowContext(ctx,
		"SELECT ref_count, pending_delete FROM blobs WHERE id = ?", blobID,
	).Scan(&refCount, &pendingDelete); err != nil {
		t.Fatalf("查 blob: %v", err)
	}
	if refCount != 1 || pendingDelete != 0 {
		t.Fatalf("ref=%d pending=%d, 期望 1/0(引用行已消失不应减计数或标待删)", refCount, pendingDelete)
	}
}
