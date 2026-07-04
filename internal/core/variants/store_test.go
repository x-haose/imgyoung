package variants_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// newStore 打开临时库、迁移,造一个 blob,返回存储、blob 主键、文件根目录。
func newStore(t *testing.T, capacity int64) (*variants.Store, int64, string) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	q := sqlc.New(conn)
	blobID, err := q.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	dir := t.TempDir()
	s := variants.New(variants.Config{Dir: dir, Capacity: capacity, Queries: q, Logger: slog.New(slog.DiscardHandler)})
	return s, blobID, dir
}

func TestStorePutGetRoundtrip(t *testing.T) {
	s, blobID, _ := newStore(t, 1<<30)
	ctx := t.Context()
	key := variants.Key(blobID, 400, "webp", 80, 4)
	want := []byte("webp-bytes")

	if err := s.Put(ctx, blobID, key, "webp", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, format, ok, err := s.Get(ctx, blobID, key)
	if err != nil || !ok {
		t.Fatalf("Get 未命中: ok=%v err=%v", ok, err)
	}
	if string(got) != string(want) || format != "webp" {
		t.Fatalf("Get 返回 data=%q format=%q, 期望 %q/webp", got, format, want)
	}
}

func TestStoreGetMiss(t *testing.T) {
	s, blobID, dir := newStore(t, 1<<30)
	ctx := t.Context()
	key := variants.Key(blobID, 100, "webp", 80, 4)

	// 从未写入:未命中。
	if _, _, ok, err := s.Get(ctx, blobID, key); ok || err != nil {
		t.Fatalf("空缓存应未命中: ok=%v err=%v", ok, err)
	}

	// 索引在但文件被手工删:仍视为未命中,交上层重建。
	if err := s.Put(ctx, blobID, key, "webp", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = os.Remove(filepath.Join(dir, key[:2], key))
	if _, _, ok, err := s.Get(ctx, blobID, key); ok || err != nil {
		t.Fatalf("文件缺失应未命中: ok=%v err=%v", ok, err)
	}
}

func TestStoreLRUEvicts(t *testing.T) {
	const each = 1000
	// 容量仅容 3 个,写 5 个后总量必须回落到容量以下。
	s, blobID, _ := newStore(t, 3*each)
	ctx := t.Context()
	payload := make([]byte, each)

	for i := 0; i < 5; i++ {
		key := variants.Key(blobID, 100+i, "webp", 80, 4)
		if err := s.Put(ctx, blobID, key, "webp", payload); err != nil {
			t.Fatalf("Put #%d: %v", i, err)
		}
	}

	// 最早写入的两个应被淘汰(命中数不超过容量能容纳的条数)。
	hits := 0
	for i := 0; i < 5; i++ {
		key := variants.Key(blobID, 100+i, "webp", 80, 4)
		if _, _, ok, _ := s.Get(ctx, blobID, key); ok {
			hits++
		}
	}
	if hits > 3 {
		t.Fatalf("淘汰后命中 %d 个,应 ≤ 3(容量上限)", hits)
	}
}

func TestStoreDeleteByBlob(t *testing.T) {
	s, blobID, dir := newStore(t, 1<<30)
	ctx := t.Context()
	key := variants.Key(blobID, 400, "webp", 80, 4)
	if err := s.Put(ctx, blobID, key, "webp", []byte("z")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.DeleteByBlob(ctx, blobID); err != nil {
		t.Fatalf("DeleteByBlob: %v", err)
	}
	if _, _, ok, _ := s.Get(ctx, blobID, key); ok {
		t.Fatal("级联删除后仍命中")
	}
	if _, err := os.Stat(filepath.Join(dir, key[:2], key)); !os.IsNotExist(err) {
		t.Fatalf("级联删除后文件仍在: %v", err)
	}
}
