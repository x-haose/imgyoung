package lifecycle_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/infra/db"
)

// fakeDeleter 记录级联删除的入队调用。
type fakeDeleter struct {
	calls []deleteCall
}

// deleteCall 一次删除入队的参数。
type deleteCall struct {
	channelID  int64
	storageKey string
}

// EnqueueDelete 记录调用。
func (f *fakeDeleter) EnqueueDelete(_ context.Context, channelID int64, storageKey string) error {
	f.calls = append(f.calls, deleteCall{channelID: channelID, storageKey: storageKey})
	return nil
}

// setup 打开临时库并迁移,返回连接与查询句柄。
func setup(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() {
		if cerr := conn.Close(); cerr != nil {
			t.Errorf("关闭测试数据库: %v", cerr)
		}
	})
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	return conn, db.New(conn)
}

// TestSweepExpiresAndCascades 验证到期引用被回收、引用归零的物理图被物理删,
// 且渠道副本级联入队删除(内置 local 渠道 id=1 由迁移播种)。
func TestSweepExpiresAndCascades(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, db.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "obj_key", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	// 已到期的引用(expire_at 在过去)。
	past := sql.NullString{String: db.TimeText(time.Now().Add(-time.Hour)), Valid: true}
	if err := q.CreateImage(ctx, db.CreateImageParams{
		ID: "img0000001", BlobID: blobID,
		UserID: sql.NullString{String: db.DefaultUserID, Valid: true}, ExpireAt: past,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 一条副本记录(渠道 1 = 内置 local),供级联删除。
	if err := q.UpsertBlobChannel(ctx, db.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: 1, State: "done",
	}); err != nil {
		t.Fatalf("建副本记录: %v", err)
	}

	deleter := &fakeDeleter{}
	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Deleter: deleter, Grace: 0, Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	// 引用应已删除。
	if _, err := q.GetImageBlob(ctx, "img0000001"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("到期引用未删除: err=%v", err)
	}
	// 物理图应已物理删(grace=0,同轮内标记+物理删)。
	if _, err := q.GetBlob(ctx, blobID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("引用归零的物理图未物理删: err=%v", err)
	}
	// 渠道副本应级联入队删除。
	if len(deleter.calls) != 1 || deleter.calls[0].channelID != 1 || deleter.calls[0].storageKey != "obj_key" {
		t.Errorf("级联删除入队异常: %+v", deleter.calls)
	}
}

// TestSweepKeepsReferencedBlob 验证 blob 仍被其他引用持有时不物理删(引用计数把关)。
func TestSweepKeepsReferencedBlob(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, db.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "shared_key", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	// 两条引用共享同一 blob:一条已到期,一条永不过期。ref_count 需为 2。
	if _, err := conn.ExecContext(ctx, "UPDATE blobs SET ref_count = 2 WHERE id = ?", blobID); err != nil {
		t.Fatalf("提升引用计数: %v", err)
	}
	uid := sql.NullString{String: db.DefaultUserID, Valid: true}
	past := sql.NullString{String: db.TimeText(time.Now().Add(-time.Hour)), Valid: true}
	if err := q.CreateImage(ctx, db.CreateImageParams{ID: "expiring01", BlobID: blobID, UserID: uid, ExpireAt: past}); err != nil {
		t.Fatalf("建到期 image: %v", err)
	}
	if err := q.CreateImage(ctx, db.CreateImageParams{ID: "permanent1", BlobID: blobID, UserID: uid}); err != nil {
		t.Fatalf("建永久 image: %v", err)
	}

	deleter := &fakeDeleter{}
	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: deleter, Grace: 0, Logger: slog.New(slog.DiscardHandler)})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	// 到期引用删除,但 blob 仍被永久引用持有,不得物理删。
	if _, err := q.GetImageBlob(ctx, "permanent1"); err != nil {
		t.Errorf("永久引用被误删: %v", err)
	}
	if _, err := q.GetBlob(ctx, blobID); err != nil {
		t.Errorf("仍被引用的物理图被误删: %v", err)
	}
	if len(deleter.calls) != 0 {
		t.Errorf("仍被引用的物理图不应级联删除: %+v", deleter.calls)
	}
}
