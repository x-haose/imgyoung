package lifecycle_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
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
func setup(t *testing.T) (*sql.DB, *sqlc.Queries) {
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
	return conn, sqlc.New(conn)
}

// TestSweepExpiresAndCascades 验证到期引用被回收、引用归零的物理图被物理删,
// 且渠道副本级联入队删除(内置 local 渠道 id=1 由迁移播种)。
func TestSweepExpiresAndCascades(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "obj_key", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	// 已到期的引用(expire_at 在过去)。
	past := sql.NullString{String: sqlc.TimeText(time.Now().Add(-time.Hour)), Valid: true}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID: "img0000001", BlobID: blobID,
		UserID: sqlc.DefaultUserID, ExpireAt: past,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 一条副本记录(渠道 1 = 内置 local),供级联删除。
	if err := q.UpsertBlobChannel(ctx, sqlc.UpsertBlobChannelParams{
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

// TestSweepCascadesVariants 验证物理删 blob 时级联清理其衍生图缓存;
// 兼验 variants 外键不会拦下 DeleteBlob(必须先删变体行)。
func TestSweepCascadesVariants(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "obj_key", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	past := sql.NullString{String: sqlc.TimeText(time.Now().Add(-time.Hour)), Valid: true}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID: "img0000002", BlobID: blobID,
		UserID: sqlc.DefaultUserID, ExpireAt: past,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 造一条衍生图缓存索引(外键指向该 blob)。
	key := variants.Key(blobID, 200, "webp", 80, 4)
	if err := q.UpsertVariant(ctx, sqlc.UpsertVariantParams{
		BlobID: blobID, ParamsHash: key, Format: "webp", Size: 10,
	}); err != nil {
		t.Fatalf("建变体索引: %v", err)
	}

	store := variants.New(variants.Config{
		Dir: t.TempDir(), Capacity: 1 << 30, Queries: q, Logger: slog.New(slog.DiscardHandler),
	})
	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Variants: store, Grace: 0,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	// blob 应已物理删(变体行先删,外键未拦)。
	if _, err := q.GetBlob(ctx, blobID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("blob 未物理删(疑被外键拦下): err=%v", err)
	}
	// 变体索引应级联删除。
	if _, err := q.GetVariant(ctx, sqlc.GetVariantParams{BlobID: blobID, ParamsHash: key}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("变体索引未级联删除: err=%v", err)
	}
}

// TestSweepKeepsReferencedBlob 验证 blob 仍被其他引用持有时不物理删(引用计数把关)。
func TestSweepKeepsReferencedBlob(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "shared_key", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	// 两条引用共享同一 blob:一条已到期,一条永不过期。ref_count 需为 2。
	if _, err := conn.ExecContext(ctx, "UPDATE blobs SET ref_count = 2 WHERE id = ?", blobID); err != nil {
		t.Fatalf("提升引用计数: %v", err)
	}
	uid := sqlc.DefaultUserID
	past := sql.NullString{String: sqlc.TimeText(time.Now().Add(-time.Hour)), Valid: true}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "expiring01", BlobID: blobID, UserID: uid, ExpireAt: past}); err != nil {
		t.Fatalf("建到期 image: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "permanent1", BlobID: blobID, UserID: uid}); err != nil {
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

// TestDeleteImageIdempotentUnderDoubleDelete 验证重复回收同一引用不多减计数:
// 共享 blob(ref=2)的一条引用被删两次,ref_count 只降到 1,blob 不被误标物理删,
// 另一条引用仍可读(防"管理端删除与到期扫描并发命中同图致数据丢失")。
func TestDeleteImageIdempotentUnderDoubleDelete(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "shared", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "UPDATE blobs SET ref_count = 2 WHERE id = ?", blobID); err != nil {
		t.Fatalf("提升引用计数: %v", err)
	}
	uid := sqlc.DefaultUserID
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "victimimg1", BlobID: blobID, UserID: uid}); err != nil {
		t.Fatalf("建被删 image: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "survivor01", BlobID: blobID, UserID: uid}); err != nil {
		t.Fatalf("建幸存 image: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Grace: 0, Logger: slog.New(slog.DiscardHandler)})

	// 首次删除:引用消失,ref_count 2→1。
	if err := sw.DeleteImage(ctx, "victimimg1"); err != nil {
		t.Fatalf("首次删除: %v", err)
	}
	// 重复删除同一引用(模拟并发第二路径):必须幂等,不再减计数。
	if err := sw.DeleteImage(ctx, "victimimg1"); !errors.Is(err, lifecycle.ErrImageNotFound) {
		t.Fatalf("重复删除应报不存在, got %v", err)
	}

	var refCount, pendingDelete int
	if err := conn.QueryRowContext(ctx, "SELECT ref_count, pending_delete FROM blobs WHERE id = ?", blobID).Scan(&refCount, &pendingDelete); err != nil {
		t.Fatalf("查 blob: %v", err)
	}
	if refCount != 1 || pendingDelete != 0 {
		t.Fatalf("重复删除后 ref=%d pending=%d, 期望 1/0(未多减未误删)", refCount, pendingDelete)
	}
	if _, err := q.GetImageBlob(ctx, "survivor01"); err != nil {
		t.Errorf("幸存引用被牵连: %v", err)
	}
}

// TestExpireOneSkipsCountWhenRowGone 直接验证 expireOne 的原子性:当引用行已被
// 并发删除(DeleteImage 影响 0 行),不得递减 ref_count。
func TestExpireOneSkipsCountWhenRowGone(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	uid := sqlc.DefaultUserID
	past := sql.NullString{String: sqlc.TimeText(time.Now().Add(-time.Hour)), Valid: true}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "ghostimg01", BlobID: blobID, UserID: uid, ExpireAt: past}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 抢先删掉引用行(但故意不减计数),模拟并发路径已删行、本轮扫描仍持旧快照。
	if _, err := conn.ExecContext(ctx, "DELETE FROM images WHERE id = 'ghostimg01'"); err != nil {
		t.Fatalf("抢先删引用: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Grace: 0, Logger: slog.New(slog.DiscardHandler)})
	// Sweeper 扫不到已删的到期引用,直接调 SweepOnce 应无副作用;ref_count 保持 1。
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	var refCount int
	if err := conn.QueryRowContext(ctx, "SELECT ref_count FROM blobs WHERE id = ?", blobID).Scan(&refCount); err != nil {
		t.Fatalf("查 ref_count: %v", err)
	}
	if refCount != 1 {
		t.Fatalf("ref_count = %d, 期望 1(引用行已消失不应减计数)", refCount)
	}
}

// TestTrackerFlushTouchesAndUnlabels 验证活跃度 flush 落库 last_public_access_at,
// 且摘除 pending_cleanup 标签(有真实访问则不再算冷图)。
func TestTrackerFlushTouchesAndUnlabels(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	uid := sqlc.DefaultUserID
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "img0000001", BlobID: blobID, UserID: uid}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 预置为待清理,验证访问后摘标签。
	if _, err := conn.ExecContext(ctx, "UPDATE images SET pending_cleanup = 1 WHERE id = ?", "img0000001"); err != nil {
		t.Fatalf("预置标签: %v", err)
	}

	tr := lifecycle.NewTracker(conn, q, false, slog.New(slog.DiscardHandler))
	tr.Touch("img0000001")
	if err := tr.Flush(ctx); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}

	var access sql.NullString
	var pending int
	row := conn.QueryRowContext(ctx, "SELECT last_public_access_at, pending_cleanup FROM images WHERE id = ?", "img0000001")
	if err := row.Scan(&access, &pending); err != nil {
		t.Fatalf("读取 image: %v", err)
	}
	if !access.Valid {
		t.Error("last_public_access_at 未落库")
	}
	if pending != 0 {
		t.Error("访问后 pending_cleanup 未摘除")
	}
}

// renewImage 建一条可定制到期/时长/豁免的引用。
func renewImage(t *testing.T, conn *sql.DB, q *sqlc.Queries, id, key string, expireAt sql.NullString, ttlSeconds sql.NullInt64, exempt bool) {
	t.Helper()
	blobID, err := q.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: key, Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: id, BlobID: blobID, UserID: sqlc.DefaultUserID, ExpireAt: expireAt, TtlSeconds: ttlSeconds,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	if exempt {
		if _, err := conn.ExecContext(t.Context(), "UPDATE images SET cleanup_exempt = 1 WHERE id = ?", id); err != nil {
			t.Fatalf("置豁免: %v", err)
		}
	}
}

// expireAtOf 读取引用的 expire_at(可空)。
func expireAtOf(t *testing.T, conn *sql.DB, id string) sql.NullString {
	t.Helper()
	var v sql.NullString
	if err := conn.QueryRowContext(t.Context(), "SELECT expire_at FROM images WHERE id = ?", id).Scan(&v); err != nil {
		t.Fatalf("读 expire_at: %v", err)
	}
	return v
}

// TestTrackerFlushRenewsTTL 验证访问即续期:flush 把本批带 TTL 的活跃引用到期时刻
// 顺延为 flush 时刻 + ttl_seconds;豁免只免除冷图打标,豁免热图同样续期;永久图不动。
func TestTrackerFlushRenewsTTL(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	soon := sql.NullString{String: sqlc.TimeText(time.Now().Add(time.Hour)), Valid: true}
	ttl := sql.NullInt64{Int64: 7200, Valid: true}
	renewImage(t, conn, q, "hotimg0001", "k1", soon, ttl, false)
	renewImage(t, conn, q, "exemptimg1", "k2", soon, ttl, true)
	renewImage(t, conn, q, "foreverimg", "k3", sql.NullString{}, sql.NullInt64{}, false)

	tr := lifecycle.NewTracker(conn, q, true, slog.New(slog.DiscardHandler))
	tr.Touch("hotimg0001")
	tr.Touch("exemptimg1")
	tr.Touch("foreverimg")
	if err := tr.Flush(ctx); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}

	// 热图与豁免热图:到期都应被推远到约 now+2h(原 now+1h)。
	want := time.Now().Add(2 * time.Hour)
	for _, id := range []string{"hotimg0001", "exemptimg1"} {
		got := expireAtOf(t, conn, id)
		if !got.Valid {
			t.Fatalf("%s expire_at 被误清空", id)
		}
		renewed, err := sqlc.ParseTimeText(got.String)
		if err != nil {
			t.Fatalf("解析 %s 续期后到期时刻: %v", id, err)
		}
		if diff := renewed.Sub(want); diff < -time.Minute || diff > time.Minute {
			t.Errorf("%s 续期后到期 = %v, 期望约 %v", id, renewed, want)
		}
	}
	// 永久图:仍无到期。
	if got := expireAtOf(t, conn, "foreverimg"); got.Valid {
		t.Errorf("永久图被误设到期: %v", got)
	}
}

// TestTrackerFlushRenewsBeyondChunk 验证续期分片:一个 flush 批超过单条语句的
// id 分片上限时,所有引用仍全部续期,无一遗漏(尾部不足一片的余量也覆盖)。
func TestTrackerFlushRenewsBeyondChunk(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	const n = 501
	soon := sql.NullString{String: sqlc.TimeText(time.Now().Add(time.Hour)), Valid: true}
	ttl := sql.NullInt64{Int64: 7200, Valid: true}
	tr := lifecycle.NewTracker(conn, q, true, slog.New(slog.DiscardHandler))
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("chunkimg%03d", i)
		renewImage(t, conn, q, id, "ck"+id, soon, ttl, false)
		tr.Touch(id)
	}
	if err := tr.Flush(ctx); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}

	var renewed int
	if err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM images WHERE id LIKE 'chunkimg%' AND expire_at > ?", soon.String,
	).Scan(&renewed); err != nil {
		t.Fatalf("统计续期行: %v", err)
	}
	if renewed != n {
		t.Errorf("续期行数 = %d, 期望 %d", renewed, n)
	}
}

// TestTrackerFlushNoRenewWhenDisabled 验证配置关闭访问续期时,flush 只记活跃度、不动到期。
func TestTrackerFlushNoRenewWhenDisabled(t *testing.T) {
	conn, q := setup(t)
	soon := sql.NullString{String: sqlc.TimeText(time.Now().Add(time.Hour)), Valid: true}
	renewImage(t, conn, q, "coldrenew1", "k1", soon, sql.NullInt64{Int64: 7200, Valid: true}, false)

	tr := lifecycle.NewTracker(conn, q, false, slog.New(slog.DiscardHandler))
	tr.Touch("coldrenew1")
	if err := tr.Flush(t.Context()); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}

	if got := expireAtOf(t, conn, "coldrenew1"); got != soon {
		t.Errorf("关闭续期仍改动到期: %v", got)
	}
	var access sql.NullString
	if err := conn.QueryRowContext(t.Context(), "SELECT last_public_access_at FROM images WHERE id = 'coldrenew1'").Scan(&access); err != nil {
		t.Fatalf("读活跃度: %v", err)
	}
	if !access.Valid {
		t.Error("关闭续期时活跃度也未落库")
	}
}

// TestLabelColdImages 验证冷图打标:超阈值未访问的引用打 pending_cleanup,新鲜引用不打。
func TestLabelColdImages(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	uid := sqlc.DefaultUserID
	mk := func(id, key string) {
		blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{Sha256: "h", Phash: 1, StorageKey: key, Mime: "image/png", Size: 1, Width: 1, Height: 1})
		if err != nil {
			t.Fatalf("建 blob: %v", err)
		}
		if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: id, BlobID: blobID, UserID: uid}); err != nil {
			t.Fatalf("建 image: %v", err)
		}
	}
	mk("coldimg001", "k1")
	mk("freshimg01", "k2")
	// 把冷图的 created_at 拨到 100 天前(它从未被访问,冷判据取 created_at)。
	old := sqlc.TimeText(time.Now().Add(-100 * 24 * time.Hour))
	if _, err := conn.ExecContext(ctx, "UPDATE images SET created_at = ? WHERE id = ?", old, "coldimg001"); err != nil {
		t.Fatalf("回拨创建时间: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, ColdThreshold: 90 * 24 * time.Hour, Logger: slog.New(slog.DiscardHandler)})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	labeled := func(id string) int {
		var p int
		if err := conn.QueryRowContext(ctx, "SELECT pending_cleanup FROM images WHERE id = ?", id).Scan(&p); err != nil {
			t.Fatalf("读取标签: %v", err)
		}
		return p
	}
	if labeled("coldimg001") != 1 {
		t.Error("冷图未打 pending_cleanup 标签")
	}
	if labeled("freshimg01") != 0 {
		t.Error("新鲜图被误打标签")
	}
}
