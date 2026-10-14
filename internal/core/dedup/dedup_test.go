package dedup_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/dedup"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// setup 打开临时库并迁移。
func setup(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	return dbtest.New(t)
}

// mkBlobWithImage 造一个 blob(指定 sha256/key)及其一条引用。
// @return blob 主键
func mkBlobWithImage(t *testing.T, ctx context.Context, q *sqlc.Queries, sha, key, imageID string) int64 {
	t.Helper()
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: sha, Phash: 1, StorageKey: key, Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID: imageID, BlobID: blobID, UserID: sqlc.DefaultUserID,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	return blobID
}

// TestMergeExactCollapsesDuplicates 验证同 sha256 的两个 blob 被合并为一个:
// 引用改指向保留 blob、引用计数并入、冗余 blob 打物理删标记。
func TestMergeExactCollapsesDuplicates(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	// 字节完全一致(同 sha256),但对象键不同(上传永远新建 blob)。
	keeperID := mkBlobWithImage(t, ctx, q, "samehash", "img_a_key", "imageaaaa1")
	redundantID := mkBlobWithImage(t, ctx, q, "samehash", "img_b_key", "imagebbbb1")

	m := dedup.New(dedup.Config{
		Conn: conn, Queries: q, Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler),
	})
	n, err := m.MergeExactOnce(ctx)
	if err != nil {
		t.Fatalf("MergeExactOnce 返回错误: %v", err)
	}
	if n != 1 {
		t.Fatalf("合并数 = %d, 期望 1", n)
	}

	// 两条引用现在都指向保留 blob。
	for _, id := range []string{"imageaaaa1", "imagebbbb1"} {
		var blobID int64
		if err := conn.QueryRowContext(ctx, "SELECT blob_id FROM images WHERE id = ?", id).Scan(&blobID); err != nil {
			t.Fatalf("查引用 %s: %v", id, err)
		}
		if blobID != keeperID {
			t.Errorf("引用 %s 指向 blob %d, 期望保留 blob %d", id, blobID, keeperID)
		}
	}

	// 保留 blob 引用计数并入为 2。
	var keeperRef, keeperPending int
	if err := conn.QueryRowContext(ctx, "SELECT ref_count, pending_delete FROM blobs WHERE id = ?", keeperID).
		Scan(&keeperRef, &keeperPending); err != nil {
		t.Fatalf("查保留 blob: %v", err)
	}
	if keeperRef != 2 || keeperPending != 0 {
		t.Errorf("保留 blob ref_count=%d pending=%d, 期望 2 / 0", keeperRef, keeperPending)
	}

	// 冗余 blob 引用归零并打物理删标记(其对象交 sweeper 清)。
	var redRef, redPending int
	if err := conn.QueryRowContext(ctx, "SELECT ref_count, pending_delete FROM blobs WHERE id = ?", redundantID).
		Scan(&redRef, &redPending); err != nil {
		t.Fatalf("查冗余 blob: %v", err)
	}
	if redRef != 0 || redPending != 1 {
		t.Errorf("冗余 blob ref_count=%d pending=%d, 期望 0 / 1", redRef, redPending)
	}
}
