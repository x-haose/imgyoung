package variants_test

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// storeEnv 一个用例的缓存存储装配:临时库上的存储实例、预置的 blob 与文件根目录,写连接与查询集供直接操作索引行。
type storeEnv struct {
	store  *variants.Store
	blobID int64
	dir    string
	conn   *sql.DB
	q      *sqlc.Queries
}

// newStore 打开临时库、迁移,造一个 blob,装配缓存存储。
func newStore(t *testing.T, capacity int64) storeEnv {
	t.Helper()
	conn, q := dbtest.New(t)
	blobID, err := q.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	dir := t.TempDir()
	s := variants.New(variants.Config{
		Dir: dir, Capacity: func() int64 { return capacity }, Queries: q, Logger: slog.New(slog.DiscardHandler),
	})
	return storeEnv{store: s, blobID: blobID, dir: dir, conn: conn, q: q}
}

func TestStorePutGetRoundtrip(t *testing.T) {
	env := newStore(t, 1<<30)
	s, blobID := env.store, env.blobID
	ctx := t.Context()
	key := variants.Key(blobID, 400, "webp", 80, 4)
	want := []byte("webp-bytes")

	if err := s.Put(ctx, blobID, key, 200, "webp", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := s.Get(ctx, blobID, key)
	if err != nil || !ok {
		t.Fatalf("Get 未命中: ok=%v err=%v", ok, err)
	}
	if string(got.Data) != string(want) || got.Format != "webp" {
		t.Fatalf("Get 返回 data=%q format=%q, 期望 %q/webp", got.Data, got.Format, want)
	}
}

// TestStoreFlushTouchesLastAccess 验证命中只记内存,Flush 才把访问时间批量落库。
func TestStoreFlushTouchesLastAccess(t *testing.T) {
	env := newStore(t, 1<<30)
	s, blobID, conn, q := env.store, env.blobID, env.conn, env.q
	ctx := t.Context()
	key := variants.Key(blobID, 400, "webp", 80, 4)
	if err := s.Put(ctx, blobID, key, 200, "webp", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// 把访问时间拨回过去,命中后落库前后的差异才可观察(datetime('now') 只有秒级精度)。
	stale := "2000-01-01 00:00:00"
	const rewind = "UPDATE variants SET last_access = ? WHERE params_hash = ?"
	if _, err := conn.ExecContext(ctx, rewind, stale, key); err != nil {
		t.Fatalf("回拨访问时间: %v", err)
	}
	if _, ok, err := s.Get(ctx, blobID, key); !ok || err != nil {
		t.Fatalf("Get 未命中: ok=%v err=%v", ok, err)
	}
	row, err := q.GetVariant(ctx, sqlc.GetVariantParams{BlobID: blobID, ParamsHash: key})
	if err != nil {
		t.Fatalf("GetVariant: %v", err)
	}
	if row.LastAccess != stale {
		t.Fatalf("命中后未 Flush 就改了访问时间: %q", row.LastAccess)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	row, err = q.GetVariant(ctx, sqlc.GetVariantParams{BlobID: blobID, ParamsHash: key})
	if err != nil {
		t.Fatalf("GetVariant: %v", err)
	}
	if row.LastAccess == stale {
		t.Fatal("Flush 后访问时间仍是旧值")
	}
}

func TestStoreGetMiss(t *testing.T) {
	env := newStore(t, 1<<30)
	s, blobID, dir := env.store, env.blobID, env.dir
	ctx := t.Context()
	key := variants.Key(blobID, 100, "webp", 80, 4)

	// 从未写入:未命中。
	if _, ok, err := s.Get(ctx, blobID, key); ok || err != nil {
		t.Fatalf("空缓存应未命中: ok=%v err=%v", ok, err)
	}

	// 索引在但文件被手工删:仍视为未命中,交上层重建。
	if err := s.Put(ctx, blobID, key, 200, "webp", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = os.Remove(filepath.Join(dir, key[:2], key))
	if _, ok, err := s.Get(ctx, blobID, key); ok || err != nil {
		t.Fatalf("文件缺失应未命中: ok=%v err=%v", ok, err)
	}
}

func TestStoreLRUEvicts(t *testing.T) {
	const each = 1000
	// 容量仅容 3 个,写 5 个后总量必须回落到容量以下。
	env := newStore(t, 3*each)
	s, blobID := env.store, env.blobID
	ctx := t.Context()
	payload := make([]byte, each)

	for i := 0; i < 5; i++ {
		key := variants.Key(blobID, 100+i, "webp", 80, 4)
		if err := s.Put(ctx, blobID, key, 200, "webp", payload); err != nil {
			t.Fatalf("Put #%d: %v", i, err)
		}
	}

	// 最早写入的两个应被淘汰(命中数不超过容量能容纳的条数)。
	hits := 0
	for i := 0; i < 5; i++ {
		key := variants.Key(blobID, 100+i, "webp", 80, 4)
		if _, ok, _ := s.Get(ctx, blobID, key); ok {
			hits++
		}
	}
	if hits > 3 {
		t.Fatalf("淘汰后命中 %d 个,应 ≤ 3(容量上限)", hits)
	}
}

func TestStoreDeleteByBlob(t *testing.T) {
	env := newStore(t, 1<<30)
	s, blobID, dir := env.store, env.blobID, env.dir
	ctx := t.Context()
	key := variants.Key(blobID, 400, "webp", 80, 4)
	if err := s.Put(ctx, blobID, key, 200, "webp", []byte("z")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.DeleteByBlob(ctx, blobID); err != nil {
		t.Fatalf("DeleteByBlob: %v", err)
	}
	if _, ok, _ := s.Get(ctx, blobID, key); ok {
		t.Fatal("级联删除后仍命中")
	}
	if _, err := os.Stat(filepath.Join(dir, key[:2], key)); !os.IsNotExist(err) {
		t.Fatalf("级联删除后文件仍在: %v", err)
	}
}
