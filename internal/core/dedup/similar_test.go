package dedup_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/dedup"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// mkBlobWithPhash 造一个指定 pHash 的 blob 及其一条引用。
// @return blob 主键
func mkBlobWithPhash(t *testing.T, ctx context.Context, q *sqlc.Queries, sha, key, imageID string, phash int64) int64 {
	t.Helper()
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: sha, Phash: phash, StorageKey: key, Mime: "image/png", Size: 3, Width: 1, Height: 1,
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

// newSimilarMerger 建一个启用相似扫描的执行器(阈值 4)。
func newSimilarMerger(conn *sql.DB, q *sqlc.Queries) *dedup.Merger {
	return dedup.New(dedup.Config{
		Conn: conn, Queries: q, Grace: 0, SimilarityThreshold: 4,
		Logger: slog.New(slog.DiscardHandler),
	})
}

// countCandidates 统计指定状态的候选行数。
func countCandidates(t *testing.T, conn *sql.DB, status string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM dedup_candidates WHERE status = ?", status).Scan(&n); err != nil {
		t.Fatalf("统计候选: %v", err)
	}
	return n
}

// TestScanSimilarFindsPairsWithinThreshold 阈值内的对入候选,阈值外不入;重复扫描幂等。
func TestScanSimilarFindsPairsWithinThreshold(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	// 0b0000 与 0b0011 距离 2(≤4 入候选);0xFFFF... 与两者距离远(不入)。
	a := mkBlobWithPhash(t, ctx, q, "sha-a", "key-a", "imageaaaa1", 0b0000)
	b := mkBlobWithPhash(t, ctx, q, "sha-b", "key-b", "imagebbbb1", 0b0011)
	mkBlobWithPhash(t, ctx, q, "sha-c", "key-c", "imagecccc1", -1)

	m := newSimilarMerger(conn, q)
	n, err := m.ScanSimilarOnce(ctx)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if n != 1 || countCandidates(t, conn, "pending") != 1 {
		t.Fatalf("候选数 = %d(库内 %d), 期望 1", n, countCandidates(t, conn, "pending"))
	}
	var blobA, blobB int64
	if err := conn.QueryRow("SELECT blob_a, blob_b FROM dedup_candidates").Scan(&blobA, &blobB); err != nil {
		t.Fatalf("查候选: %v", err)
	}
	if blobA != a || blobB != b {
		t.Fatalf("候选对 = (%d,%d), 期望 (%d,%d)", blobA, blobB, a, b)
	}

	// 水位之后重复扫描不产生新候选。
	if n, err := m.ScanSimilarOnce(ctx); err != nil || n != 0 {
		t.Fatalf("重复扫描应无新候选: n=%d err=%v", n, err)
	}
}

// TestScanSimilarDismissAbsorbsRescan 忽略后即便水位重置(进程重启)重扫,也不再出现。
func TestScanSimilarDismissAbsorbsRescan(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	mkBlobWithPhash(t, ctx, q, "sha-a", "key-a", "imageaaaa1", 0)
	mkBlobWithPhash(t, ctx, q, "sha-b", "key-b", "imagebbbb1", 1)

	m := newSimilarMerger(conn, q)
	if _, err := m.ScanSimilarOnce(ctx); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	var candID int64
	if err := conn.QueryRow("SELECT id FROM dedup_candidates").Scan(&candID); err != nil {
		t.Fatalf("查候选: %v", err)
	}
	if err := m.DismissCandidate(ctx, candID); err != nil {
		t.Fatalf("忽略失败: %v", err)
	}
	if err := m.DismissCandidate(ctx, candID); !errors.Is(err, dedup.ErrCandidateNotFound) {
		t.Fatalf("重复忽略应不存在, got %v", err)
	}

	// 新执行器 = 水位归零全量重扫:唯一约束吸收,dismissed 不复活。
	m2 := newSimilarMerger(conn, q)
	if n, err := m2.ScanSimilarOnce(ctx); err != nil || n != 0 {
		t.Fatalf("重扫应被吸收: n=%d err=%v", n, err)
	}
	if countCandidates(t, conn, "dismissed") != 1 || countCandidates(t, conn, "pending") != 0 {
		t.Fatal("忽略状态应保留且无新 pending")
	}
}

// TestMergeCandidateRepointsAndCleans 裁决合并:引用改指向保留方、计数并入、
// 被并方标记待删、涉其候选全部出队。
func TestMergeCandidateRepointsAndCleans(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	a := mkBlobWithPhash(t, ctx, q, "sha-a", "key-a", "imageaaaa1", 0)
	b := mkBlobWithPhash(t, ctx, q, "sha-b", "key-b", "imagebbbb1", 1)

	m := newSimilarMerger(conn, q)
	if _, err := m.ScanSimilarOnce(ctx); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	var candID int64
	if err := conn.QueryRow("SELECT id FROM dedup_candidates").Scan(&candID); err != nil {
		t.Fatalf("查候选: %v", err)
	}

	if err := m.MergeCandidate(ctx, candID, 999); !errors.Is(err, dedup.ErrBadKeeper) {
		t.Fatalf("非候选方应拒绝, got %v", err)
	}
	if err := m.MergeCandidate(ctx, candID, b); err != nil {
		t.Fatalf("合并失败: %v", err)
	}

	var blobID int64
	if err := conn.QueryRow("SELECT blob_id FROM images WHERE id = 'imageaaaa1'").Scan(&blobID); err != nil {
		t.Fatalf("查引用: %v", err)
	}
	if blobID != b {
		t.Fatalf("引用指向 %d, 期望保留方 %d", blobID, b)
	}
	var refCount, pendingDelete int
	if err := conn.QueryRow("SELECT ref_count, pending_delete FROM blobs WHERE id = ?", b).Scan(&refCount, &pendingDelete); err != nil {
		t.Fatalf("查保留方: %v", err)
	}
	if refCount != 2 || pendingDelete != 0 {
		t.Fatalf("保留方 ref=%d pending=%d, 期望 2/0", refCount, pendingDelete)
	}
	if err := conn.QueryRow("SELECT pending_delete FROM blobs WHERE id = ?", a).Scan(&pendingDelete); err != nil {
		t.Fatalf("查被并方: %v", err)
	}
	if pendingDelete != 1 {
		t.Fatal("被并方应打物理删标记")
	}
	if n := countCandidates(t, conn, "pending"); n != 0 {
		t.Fatalf("候选应全部出队, 剩 %d", n)
	}
	if err := m.MergeCandidate(ctx, candID, b); !errors.Is(err, dedup.ErrCandidateNotFound) {
		t.Fatalf("已出队候选应不存在, got %v", err)
	}
}

// TestMergeCandidateStaleBlob 候选一方已进入删除流程时拒绝合并。
func TestMergeCandidateStaleBlob(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	a := mkBlobWithPhash(t, ctx, q, "sha-a", "key-a", "imageaaaa1", 0)
	b := mkBlobWithPhash(t, ctx, q, "sha-b", "key-b", "imagebbbb1", 1)

	m := newSimilarMerger(conn, q)
	if _, err := m.ScanSimilarOnce(ctx); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	var candID int64
	if err := conn.QueryRow("SELECT id FROM dedup_candidates").Scan(&candID); err != nil {
		t.Fatalf("查候选: %v", err)
	}
	if _, err := conn.Exec("UPDATE blobs SET pending_delete = 1 WHERE id = ?", a); err != nil {
		t.Fatalf("标记删除: %v", err)
	}
	if err := m.MergeCandidate(ctx, candID, b); !errors.Is(err, dedup.ErrCandidateStale) {
		t.Fatalf("失效候选应拒绝, got %v", err)
	}
}

// TestScanSimilarDisabled 阈值为 0 时相似扫描完全关闭。
func TestScanSimilarDisabled(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	mkBlobWithPhash(t, ctx, q, "sha-a", "key-a", "imageaaaa1", 0)
	mkBlobWithPhash(t, ctx, q, "sha-b", "key-b", "imagebbbb1", 0)

	m := dedup.New(dedup.Config{Conn: conn, Queries: q, Logger: slog.New(slog.DiscardHandler)})
	if n, err := m.ScanSimilarOnce(ctx); err != nil || n != 0 {
		t.Fatalf("关闭时不应产生候选: n=%d err=%v", n, err)
	}
}
