package lifecycle_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// testSettings 构造运营配置读取口:内嵌默认值上按需覆写。
func testSettings(t *testing.T, mod func(*config.Settings)) func() config.Settings {
	t.Helper()
	set := config.Defaults()
	if mod != nil {
		mod(&set)
	}
	return func() config.Settings { return set }
}

// purgeSettings 构造启用 CDN purge 的配置读取口(zone / token / base_url 三者齐备)。
func purgeSettings(t *testing.T) func() config.Settings {
	t.Helper()
	return testSettings(t, func(s *config.Settings) {
		s.BaseURL = "https://img.example.com"
		s.CDNCloudflareZoneID = "zone"
		s.CDNCloudflareAPIToken = "token"
	})
}

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

// fakePurger 记录 CDN purge 入队调用,可注入失败。
type fakePurger struct {
	batches [][]string // 每次入队的 URL 列表
	err     error      // 非 nil 时入队返回该错误
}

// EnqueuePurge 记录调用。
func (f *fakePurger) EnqueuePurge(_ context.Context, urls []string) error {
	f.batches = append(f.batches, urls)
	return f.err
}

// setup 打开临时库并迁移,返回连接与查询句柄。
func setup(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	return dbtest.New(t)
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
		Conn: conn, Queries: q, Deleter: deleter, Settings: testSettings(t, nil),
		Logger: slog.New(slog.DiscardHandler),
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
		Dir: t.TempDir(), Capacity: func() int64 { return 1 << 30 }, Queries: q,
		Logger: slog.New(slog.DiscardHandler),
	})
	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Variants: store,
		Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler),
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

// TestDeleteCascadesCDNPurge 验证引用删除级联收集并入队 CDN purge URL:
// 原图 /i/{id} + 该 blob 实际存在过的变体参数组合(显式与协商两种形态,去重)。
func TestDeleteCascadesCDNPurge(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID: "purgeimg01", BlobID: blobID, UserID: sqlc.DefaultUserID,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	// 两个变体:缩放+转格式、仅转格式。
	for _, v := range []struct {
		width  int64
		format string
	}{{200, "webp"}, {0, "webp"}} {
		if err := q.UpsertVariant(ctx, sqlc.UpsertVariantParams{
			BlobID: blobID, ParamsHash: fmt.Sprintf("ph-%d-%s", v.width, v.format),
			Width: v.width, Format: v.format, Size: 1,
		}); err != nil {
			t.Fatalf("建变体索引: %v", err)
		}
	}

	purger := &fakePurger{}
	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Purger: purger,
		Settings: purgeSettings(t), Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.DeleteImage(ctx, "purgeimg01"); err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}

	if len(purger.batches) != 1 {
		t.Fatalf("purge 入队次数 = %d, 期望 1", len(purger.batches))
	}
	got := map[string]bool{}
	for _, u := range purger.batches[0] {
		got[u] = true
	}
	want := []string{
		"https://img.example.com/i/purgeimg01",
		"https://img.example.com/i/purgeimg01?w=200",
		"https://img.example.com/i/purgeimg01?w=200&format=webp",
		"https://img.example.com/i/purgeimg01?format=webp",
	}
	if len(got) != len(want) {
		t.Errorf("purge URL 集大小 = %d, 期望 %d: %v", len(got), len(want), purger.batches[0])
	}
	for _, u := range want {
		if !got[u] {
			t.Errorf("purge URL 缺失: %s", u)
		}
	}
}

// TestDeleteToleratesPurgeEnqueueFailure 验证 purge 入队失败只告警,不阻塞删除。
func TestDeleteToleratesPurgeEnqueueFailure(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID: "purgefail01", BlobID: blobID, UserID: sqlc.DefaultUserID,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Deleter: &fakeDeleter{},
		Purger:   &fakePurger{err: errors.New("队列不可用")},
		Settings: purgeSettings(t), Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.DeleteImage(ctx, "purgefail01"); err != nil {
		t.Fatalf("purge 入队失败不应阻塞删除: %v", err)
	}
	if _, err := q.GetImageBlob(ctx, "purgefail01"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("引用未删除: err=%v", err)
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
	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: deleter, Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler)})
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

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler)})

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

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: &fakeDeleter{}, Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler)})
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

// TestTrackerFlushTouchesAndUnlabels 验证活跃度 flush 落库 last_public_access_at
// 与 access_count(同周期多次访问计 1,跨周期累加),且摘除 pending_cleanup 标签
// (有真实访问则不再算冷图)。
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

	tr := lifecycle.NewTracker(conn, q, func() bool { return false }, slog.New(slog.DiscardHandler))
	// 同一 flush 周期内多次 Touch 只计一次(内存集合去重即计数口径)。
	tr.Touch("img0000001")
	tr.Touch("img0000001")
	if err := tr.Flush(ctx); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}

	var access sql.NullString
	var pending, count int
	row := conn.QueryRowContext(ctx, "SELECT last_public_access_at, pending_cleanup, access_count FROM images WHERE id = ?", "img0000001")
	if err := row.Scan(&access, &pending, &count); err != nil {
		t.Fatalf("读取 image: %v", err)
	}
	if !access.Valid {
		t.Error("last_public_access_at 未落库")
	}
	if pending != 0 {
		t.Error("访问后 pending_cleanup 未摘除")
	}
	if count != 1 {
		t.Errorf("access_count = %d, 期望 1(同周期去重)", count)
	}

	// 下一周期再访问:计数跨周期累加。
	tr.Touch("img0000001")
	if err := tr.Flush(ctx); err != nil {
		t.Fatalf("二次 Flush 返回错误: %v", err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT access_count FROM images WHERE id = ?", "img0000001").Scan(&count); err != nil {
		t.Fatalf("读取 access_count: %v", err)
	}
	if count != 2 {
		t.Errorf("二次 flush 后 access_count = %d, 期望 2", count)
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

	tr := lifecycle.NewTracker(conn, q, func() bool { return true }, slog.New(slog.DiscardHandler))
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
	tr := lifecycle.NewTracker(conn, q, func() bool { return true }, slog.New(slog.DiscardHandler))
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

	tr := lifecycle.NewTracker(conn, q, func() bool { return false }, slog.New(slog.DiscardHandler))
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

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler)})
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

// TestSweepExpiresIdempotencyKeys 验证幂等键按保留期回收:超期的键删除,期内的键保留。
func TestSweepExpiresIdempotencyKeys(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{ID: "idemimg001", BlobID: blobID, UserID: sqlc.DefaultUserID}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	for _, key := range []string{"stale", "fresh"} {
		if err := q.CreateIdempotencyKey(ctx, sqlc.CreateIdempotencyKeyParams{
			UserID: sqlc.DefaultUserID, Key: key, ImageID: "idemimg001",
		}); err != nil {
			t.Fatalf("建幂等键 %s: %v", key, err)
		}
	}
	old := sqlc.TimeText(time.Now().Add(-48 * time.Hour))
	if _, err := conn.ExecContext(ctx, "UPDATE idempotency_keys SET created_at = ? WHERE key = 'stale'", old); err != nil {
		t.Fatalf("回拨创建时间: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler)})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}
	if _, err := q.GetIdempotencyImage(ctx, sqlc.GetIdempotencyImageParams{UserID: sqlc.DefaultUserID, Key: "stale"}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("超期幂等键未回收: err=%v", err)
	}
	if _, err := q.GetIdempotencyImage(ctx, sqlc.GetIdempotencyImageParams{UserID: sqlc.DefaultUserID, Key: "fresh"}); err != nil {
		t.Errorf("期内幂等键被误删: %v", err)
	}
}

// TestPhysicalDeleteRefusesReferencedBlob 验证物理删在事务内复查引用计数:
// 扫描到的待删 blob 若又有了引用,删除放弃、副本记录保留。
func TestPhysicalDeleteRefusesReferencedBlob(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256: "h", Phash: 1, StorageKey: "revived", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	// 待删标记在、宽限已过,但 ref_count 仍为 1(被重新引用)。
	past := sqlc.TimeText(time.Now().Add(-time.Hour))
	if _, err := conn.ExecContext(ctx, "UPDATE blobs SET pending_delete = 1, delete_after = ? WHERE id = ?", past, blobID); err != nil {
		t.Fatalf("置待删标记: %v", err)
	}
	if err := q.UpsertBlobChannel(ctx, sqlc.UpsertBlobChannelParams{BlobID: blobID, ChannelID: 1, State: "done"}); err != nil {
		t.Fatalf("建副本记录: %v", err)
	}

	deleter := &fakeDeleter{}
	sw := lifecycle.New(lifecycle.Config{Conn: conn, Queries: q, Deleter: deleter, Settings: testSettings(t, nil), Logger: slog.New(slog.DiscardHandler)})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}
	if _, err := q.GetBlob(ctx, blobID); err != nil {
		t.Fatalf("仍被引用的物理图被物理删: %v", err)
	}
	if _, err := q.GetBlobChannel(ctx, sqlc.GetBlobChannelParams{BlobID: blobID, ChannelID: 1}); err != nil {
		t.Fatalf("放弃物理删后副本记录应随事务回滚保留: %v", err)
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("放弃物理删不应入队渠道对象删除: %+v", deleter.calls)
	}
}
