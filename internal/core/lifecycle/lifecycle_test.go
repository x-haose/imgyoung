package lifecycle_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/replication"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/cdn"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// cascadeDeadline 等待补齐子系统的 worker 消化一个删除任务的上限;River 的 SQLite 驱动按秒级轮询认领任务。
const cascadeDeadline = 15 * time.Second

// purgeSettings 构造启用 CDN purge 的配置读取口(zone / token / base_url 三者齐备)。
func purgeSettings() func() config.Settings {
	return testenv.Settings(func(s *config.Settings) {
		s.BaseURL = "https://img.example.com"
		s.CDNCloudflareZoneID = "zone"
		s.CDNCloudflareAPIToken = "token"
	})
}

// sweeperEnv 清理器测试装配:临时库、内置 local 渠道与真实的补齐子系统。
// @Description: 级联入队的断言落在真实结果上——启动 worker 后 local 渠道上的对象文件被删,
// 或 River 任务表里出现对应任务行;不记录调用的替身。
type sweeperEnv struct {
	*testenv.Env
	runner *replication.Runner // 补齐子系统;调用 start 之后 worker 才消费任务
	local  channel.Handle      // 内置 local 渠道,对象落 DataDir/blobs
}

// newSweeperEnv 装配清理器测试环境。
// @param purger CDN 缓存清理执行器;nil 表示 purge 未启用,EnqueuePurge 为空操作
func newSweeperEnv(t *testing.T, purger replication.ObjectPurger) *sweeperEnv {
	t.Helper()
	env := testenv.New(t)
	runner, err := replication.NewRunner(replication.RunnerConfig{
		Conn: env.Conn, Queries: env.Queries, Manager: env.Manager, Purger: purger, Logger: env.Logger,
	})
	if err != nil {
		t.Fatalf("创建补齐子系统: %v", err)
	}
	local, err := env.Manager.Primary()
	if err != nil {
		t.Fatalf("取内置 local 渠道: %v", err)
	}
	return &sweeperEnv{Env: env, runner: runner, local: local}
}

// start 启动补齐子系统的 worker,随测试结束停止。
// @Description: 须在造数据之前调用:启动时的恢复扫描会为缺副本行的 blob 补种 pending 行并入队补齐。
func (e *sweeperEnv) start(t *testing.T) {
	t.Helper()
	if err := e.runner.Start(t.Context()); err != nil {
		t.Fatalf("启动补齐子系统: %v", err)
	}
	t.Cleanup(func() {
		if err := e.runner.Stop(context.Background()); err != nil {
			t.Errorf("停止补齐子系统: %v", err)
		}
	})
}

// sweeper 以装配的库与补齐子系统构造清理器。
// @param settings 运营配置读取口
func (e *sweeperEnv) sweeper(settings func() config.Settings) *lifecycle.Sweeper {
	return lifecycle.New(lifecycle.Config{
		Conn: e.Conn, Queries: e.Queries, Deleter: e.runner, Purger: e.runner,
		Settings: settings, Logger: e.Logger,
	})
}

// putObject 把一个小对象写入 local 渠道。
// @param key 对象键
func (e *sweeperEnv) putObject(t *testing.T, key string) {
	t.Helper()
	payload := []byte("object-bytes")
	if err := e.local.Driver.Put(t.Context(), key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("写入渠道对象 %q: %v", key, err)
	}
}

// objectExists 报告 local 渠道上是否还有该对象的文件。
// @param key 对象键
func (e *sweeperEnv) objectExists(t *testing.T, key string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(e.DataDir, "blobs", key))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("检查渠道对象 %q: %v", key, err)
	}
	return err == nil
}

// jobCount 返回 River 任务表里指定类型的任务数。
// @param kind 任务类型标识
func (e *sweeperEnv) jobCount(t *testing.T, kind string) int {
	t.Helper()
	var n int
	if err := e.Conn.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM river_job WHERE kind = ?", kind).Scan(&n); err != nil {
		t.Fatalf("统计 %s 任务: %v", kind, err)
	}
	return n
}

// purgeStub 收集 purge 调用的本地 Cloudflare API。
type purgeStub struct {
	mu      sync.Mutex
	batches [][]string // 每次调用收到的 files 列表
}

// ServeHTTP 记录一批 URL 并回成功信封。
func (s *purgeStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.batches = append(s.batches, req.Files)
	s.mu.Unlock()
	_, _ = w.Write([]byte(`{"success":true}`))
}

// received 返回已收到的批次副本。
func (s *purgeStub) received() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.batches...)
}

// setup 打开临时库并迁移,供只需要数据库的用例使用。
func setup(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	return dbtest.New(t)
}

// createBlob 造一个指向指定对象键的物理图。
// @return 物理图主键
func createBlob(t *testing.T, q *sqlc.Queries, key string) int64 {
	t.Helper()
	blobID, err := q.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "h-" + key, Phash: 1, StorageKey: key, Mime: "image/png", Size: 3, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	return blobID
}

// createImage 造一条引用;expireAt 为零值表示永不过期。
func createImage(t *testing.T, q *sqlc.Queries, id string, blobID int64, expireAt sql.NullString) {
	t.Helper()
	if err := q.CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: id, BlobID: blobID, UserID: sqlc.DefaultUserID, ExpireAt: expireAt,
	}); err != nil {
		t.Fatalf("建 image %s: %v", id, err)
	}
}

// markReplicaDone 把物理图在渠道上的副本记为就绪。
func markReplicaDone(t *testing.T, q *sqlc.Queries, blobID, channelID int64) {
	t.Helper()
	if err := q.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: channelID, State: channel.StateDone,
	}); err != nil {
		t.Fatalf("建副本记录: %v", err)
	}
}

// pastExpiry 返回一个已过去的到期时刻。
func pastExpiry() sql.NullString {
	return sql.NullString{String: sqlc.TimeText(time.Now().Add(-time.Hour)), Valid: true}
}

// blobRow 物理图行上与删除流程相关的列。
type blobRow struct {
	refCount      int            // 引用计数
	pendingDelete int            // 待物理删标记
	deleteAfter   sql.NullString // 物理删执行时刻
}

// readBlob 读物理图行的删除相关列;行不存在时 exists 为 false。
func readBlob(t *testing.T, conn *sql.DB, blobID int64) (row blobRow, exists bool) {
	t.Helper()
	err := conn.QueryRowContext(t.Context(),
		"SELECT ref_count, pending_delete, delete_after FROM blobs WHERE id = ?", blobID,
	).Scan(&row.refCount, &row.pendingDelete, &row.deleteAfter)
	if errors.Is(err, sql.ErrNoRows) {
		return blobRow{}, false
	}
	if err != nil {
		t.Fatalf("查 blob: %v", err)
	}
	return row, true
}

// waitUntil 轮询直到条件满足,超时即失败。
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待%s超时(%s)", what, timeout)
}

// TestSweepExpiresAndCascades 验证到期引用被回收、引用归零的物理图被物理删,
// 且渠道副本经补齐子系统级联删除:local 渠道上的对象文件随之消失。
func TestSweepExpiresAndCascades(t *testing.T) {
	env := newSweeperEnv(t, nil)
	env.start(t)
	env.putObject(t, "obj_key")
	blobID := createBlob(t, env.Queries, "obj_key")
	createImage(t, env.Queries, "img0000001", blobID, pastExpiry())
	markReplicaDone(t, env.Queries, blobID, env.local.ID)

	if err := env.sweeper(testenv.Settings(nil)).SweepOnce(t.Context()); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	if _, err := env.Queries.GetImageBlobID(t.Context(), "img0000001"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("到期引用未删除: err=%v", err)
	}
	// 宽限为 0:同一轮内标记并物理删。
	if _, exists := readBlob(t, env.Conn, blobID); exists {
		t.Error("引用归零的物理图未物理删")
	}
	waitUntil(t, cascadeDeadline, "渠道对象被级联删除", func() bool { return !env.objectExists(t, "obj_key") })
}

// TestSweepGracePeriodDefersPhysicalDelete 验证宽限期:引用归零只打待删标记、物理删时刻落在宽限之后,
// 对象与副本记录原样保留;宽限到期后的下一轮才物理删并级联删对象。
func TestSweepGracePeriodDefersPhysicalDelete(t *testing.T) {
	env := newSweeperEnv(t, nil)
	env.start(t)
	env.putObject(t, "graced_key")
	blobID := createBlob(t, env.Queries, "graced_key")
	createImage(t, env.Queries, "graceimg01", blobID, pastExpiry())
	markReplicaDone(t, env.Queries, blobID, env.local.ID)

	sw := env.sweeper(testenv.Settings(func(s *config.Settings) { s.DeleteGraceMinutes = 60 }))
	before := time.Now()
	if err := sw.SweepOnce(t.Context()); err != nil {
		t.Fatalf("首轮 SweepOnce 返回错误: %v", err)
	}
	row, exists := readBlob(t, env.Conn, blobID)
	if !exists {
		t.Fatal("宽限期内物理图被物理删")
	}
	if row.refCount != 0 || row.pendingDelete != 1 || !row.deleteAfter.Valid {
		t.Fatalf("归零后物理图状态 = %+v, 期望 ref=0 且已打待删标记", row)
	}
	deleteAfter, err := sqlc.ParseTimeText(row.deleteAfter.String)
	if err != nil {
		t.Fatalf("解析物理删时刻: %v", err)
	}
	if got := deleteAfter.Sub(before); got < 59*time.Minute || got > 61*time.Minute {
		t.Fatalf("物理删时刻距回收 %v, 期望约 60 分钟宽限", got)
	}
	if env.jobCount(t, replication.DeleteObjectArgs{}.Kind()) != 0 || !env.objectExists(t, "graced_key") {
		t.Fatal("宽限期内不应入队删除渠道对象")
	}

	// 宽限到期:把物理删时刻拨到过去,下一轮物理删并级联删对象。
	past := sqlc.TimeText(time.Now().Add(-time.Minute))
	if _, err := env.Conn.ExecContext(t.Context(),
		"UPDATE blobs SET delete_after = ? WHERE id = ?", past, blobID); err != nil {
		t.Fatalf("回拨物理删时刻: %v", err)
	}
	if err := sw.SweepOnce(t.Context()); err != nil {
		t.Fatalf("次轮 SweepOnce 返回错误: %v", err)
	}
	if _, exists := readBlob(t, env.Conn, blobID); exists {
		t.Fatal("宽限到期后物理图未物理删")
	}
	waitUntil(t, cascadeDeadline, "渠道对象被级联删除", func() bool { return !env.objectExists(t, "graced_key") })
}

// TestSweepCascadesVariants 验证物理删 blob 时级联清理其衍生图缓存;
// 兼验 variants 外键不会拦下 DeleteBlob(必须先删变体行)。
func TestSweepCascadesVariants(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID := createBlob(t, q, "obj_key")
	createImage(t, q, "img0000002", blobID, pastExpiry())
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
		Conn: conn, Queries: q, Variants: store,
		Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	// blob 应已物理删(变体行先删,外键未拦)。
	if _, exists := readBlob(t, conn, blobID); exists {
		t.Error("blob 未物理删(疑被外键拦下)")
	}
	// 变体索引应级联删除。
	if _, err := q.GetVariant(ctx,
		sqlc.GetVariantParams{BlobID: blobID, ParamsHash: key}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("变体索引未级联删除: err=%v", err)
	}
}

// TestDeleteCascadesCDNPurge 验证引用删除级联清理 CDN 缓存:purge 请求里是原图 /i/{id}
// 加该 blob 实际存在过的变体参数组合(显式与协商两种形态,去重),由补齐子系统的 worker 真实外呼。
func TestDeleteCascadesCDNPurge(t *testing.T) {
	stub := &purgeStub{}
	api := httptest.NewServer(stub)
	t.Cleanup(api.Close)
	env := newSweeperEnv(t, cdn.NewCloudflareAt(api.URL, "zone", "token"))
	env.start(t)
	ctx := t.Context()
	blobID := createBlob(t, env.Queries, "k")
	createImage(t, env.Queries, "purgeimg01", blobID, sql.NullString{})
	// 两个变体:缩放+转格式、仅转格式。
	for _, v := range []struct {
		width  int64
		format string
	}{{200, "webp"}, {0, "webp"}} {
		if err := env.Queries.UpsertVariant(ctx, sqlc.UpsertVariantParams{
			BlobID: blobID, ParamsHash: fmt.Sprintf("ph-%d-%s", v.width, v.format),
			Width: v.width, Format: v.format, Size: 1,
		}); err != nil {
			t.Fatalf("建变体索引: %v", err)
		}
	}

	if err := env.sweeper(purgeSettings()).DeleteImage(ctx, "purgeimg01"); err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}

	waitUntil(t, cascadeDeadline, "purge 外呼到达", func() bool { return len(stub.received()) > 0 })
	batches := stub.received()
	if len(batches) != 1 {
		t.Fatalf("purge 调用次数 = %d, 期望 1", len(batches))
	}
	got := map[string]bool{}
	for _, u := range batches[0] {
		got[u] = true
	}
	want := []string{
		"https://img.example.com/i/purgeimg01",
		"https://img.example.com/i/purgeimg01?w=200",
		"https://img.example.com/i/purgeimg01?w=200&format=webp",
		"https://img.example.com/i/purgeimg01?format=webp",
	}
	if len(got) != len(want) {
		t.Errorf("purge URL 集大小 = %d, 期望 %d: %v", len(got), len(want), batches[0])
	}
	for _, u := range want {
		if !got[u] {
			t.Errorf("purge URL 缺失: %s", u)
		}
	}
}

// TestDeleteToleratesPurgeEnqueueFailure 验证 purge 入队失败只告警,不阻塞删除:
// 任务表缺失让入队真实报错,告警落日志,引用仍被删除。
func TestDeleteToleratesPurgeEnqueueFailure(t *testing.T) {
	env := newSweeperEnv(t, cdn.NewCloudflare("zone", "token"))
	ctx := t.Context()
	blobID := createBlob(t, env.Queries, "k")
	createImage(t, env.Queries, "purgefail01", blobID, sql.NullString{})
	if _, err := env.Conn.ExecContext(ctx, "DROP TABLE river_job"); err != nil {
		t.Fatalf("删任务表: %v", err)
	}

	var logBuf bytes.Buffer
	sw := lifecycle.New(lifecycle.Config{
		Conn: env.Conn, Queries: env.Queries, Deleter: env.runner, Purger: env.runner,
		Settings: purgeSettings(), Logger: slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	if err := sw.DeleteImage(ctx, "purgefail01"); err != nil {
		t.Fatalf("purge 入队失败不应阻塞删除: %v", err)
	}
	if !strings.Contains(logBuf.String(), "CDN purge 入队失败") {
		t.Fatalf("入队失败未告警,日志:\n%s", logBuf.String())
	}
	if _, err := env.Queries.GetImageBlobID(ctx, "purgefail01"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("引用未删除: err=%v", err)
	}
}

// TestSweepKeepsReferencedBlob 验证 blob 仍被其他引用持有时不物理删(引用计数把关),
// 渠道对象不入队删除。
func TestSweepKeepsReferencedBlob(t *testing.T) {
	env := newSweeperEnv(t, nil)
	env.start(t)
	env.putObject(t, "shared_key")
	blobID := createBlob(t, env.Queries, "shared_key")
	markReplicaDone(t, env.Queries, blobID, env.local.ID)
	// 两条引用共享同一 blob:一条已到期,一条永不过期。ref_count 需为 2。
	if _, err := env.Conn.ExecContext(t.Context(), "UPDATE blobs SET ref_count = 2 WHERE id = ?", blobID); err != nil {
		t.Fatalf("提升引用计数: %v", err)
	}
	createImage(t, env.Queries, "expiring01", blobID, pastExpiry())
	createImage(t, env.Queries, "permanent1", blobID, sql.NullString{})

	if err := env.sweeper(testenv.Settings(nil)).SweepOnce(t.Context()); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	// 到期引用删除,但 blob 仍被永久引用持有,不得物理删。
	if _, err := env.Queries.GetImageBlobID(t.Context(), "permanent1"); err != nil {
		t.Errorf("永久引用被误删: %v", err)
	}
	row, exists := readBlob(t, env.Conn, blobID)
	if !exists || row.refCount != 1 || row.pendingDelete != 0 {
		t.Errorf("仍被引用的物理图状态 = %+v(存在 %v), 期望 ref=1 且未标待删", row, exists)
	}
	if env.jobCount(t, replication.DeleteObjectArgs{}.Kind()) != 0 || !env.objectExists(t, "shared_key") {
		t.Error("仍被引用的物理图不应级联删除渠道对象")
	}
}

// TestDeleteImageIdempotentUnderDoubleDelete 验证重复回收同一引用不多减计数:
// 共享 blob(ref=2)的一条引用被删两次,ref_count 只降到 1,blob 不被误标物理删,
// 另一条引用仍可读(防"管理端删除与到期扫描并发命中同图致数据丢失")。
func TestDeleteImageIdempotentUnderDoubleDelete(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()

	blobID := createBlob(t, q, "shared")
	if _, err := conn.ExecContext(ctx, "UPDATE blobs SET ref_count = 2 WHERE id = ?", blobID); err != nil {
		t.Fatalf("提升引用计数: %v", err)
	}
	createImage(t, q, "victimimg1", blobID, sql.NullString{})
	createImage(t, q, "survivor01", blobID, sql.NullString{})

	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler),
	})

	// 首次删除:引用消失,ref_count 2→1。
	if err := sw.DeleteImage(ctx, "victimimg1"); err != nil {
		t.Fatalf("首次删除: %v", err)
	}
	// 重复删除同一引用(模拟并发第二路径):必须幂等,不再减计数。
	if err := sw.DeleteImage(ctx, "victimimg1"); !errors.Is(err, lifecycle.ErrImageNotFound) {
		t.Fatalf("重复删除应报不存在, got %v", err)
	}

	row, exists := readBlob(t, conn, blobID)
	if !exists || row.refCount != 1 || row.pendingDelete != 0 {
		t.Fatalf("重复删除后 blob = %+v(存在 %v), 期望 ref=1 且未标待删(未多减未误删)", row, exists)
	}
	if _, err := q.GetImageBlobID(ctx, "survivor01"); err != nil {
		t.Errorf("幸存引用被牵连: %v", err)
	}
}

// TestTrackerFlushTouchesAndUnlabels 验证活跃度 flush 落库 last_public_access_at
// 与 access_count(同周期多次访问计 1,跨周期累加),且摘除 pending_cleanup 标签
// (有真实访问则不再算冷图)。
func TestTrackerFlushTouchesAndUnlabels(t *testing.T) {
	conn, q := setup(t)
	ctx := t.Context()
	blobID := createBlob(t, q, "k")
	createImage(t, q, "img0000001", blobID, sql.NullString{})
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
	row := conn.QueryRowContext(ctx,
		"SELECT last_public_access_at, pending_cleanup, access_count FROM images WHERE id = ?", "img0000001")
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
	if err := conn.QueryRowContext(ctx,
		"SELECT access_count FROM images WHERE id = ?", "img0000001",
	).Scan(&count); err != nil {
		t.Fatalf("读取 access_count: %v", err)
	}
	if count != 2 {
		t.Errorf("二次 flush 后 access_count = %d, 期望 2", count)
	}
}

// renewImage 建一条可定制到期/时长/豁免的引用。
func renewImage(t *testing.T, conn *sql.DB, q *sqlc.Queries, id, key string,
	expireAt sql.NullString, ttlSeconds sql.NullInt64, exempt bool) {
	t.Helper()
	blobID := createBlob(t, q, key)
	if err := q.CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: id, BlobID: blobID, UserID: sqlc.DefaultUserID, ExpireAt: expireAt, TtlSeconds: ttlSeconds,
	}); err != nil {
		t.Fatalf("建 image: %v", err)
	}
	if exempt {
		if _, err := conn.ExecContext(t.Context(),
			"UPDATE images SET cleanup_exempt = 1 WHERE id = ?", id); err != nil {
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
	if err := conn.QueryRowContext(t.Context(),
		"SELECT last_public_access_at FROM images WHERE id = 'coldrenew1'",
	).Scan(&access); err != nil {
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
	createImage(t, q, "coldimg001", createBlob(t, q, "k1"), sql.NullString{})
	createImage(t, q, "freshimg01", createBlob(t, q, "k2"), sql.NullString{})
	// 把冷图的 created_at 拨到 100 天前(它从未被访问,冷判据取 created_at)。
	old := sqlc.TimeText(time.Now().Add(-100 * 24 * time.Hour))
	if _, err := conn.ExecContext(ctx, "UPDATE images SET created_at = ? WHERE id = ?", old, "coldimg001"); err != nil {
		t.Fatalf("回拨创建时间: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}

	labeled := func(id string) int {
		var p int
		if err := conn.QueryRowContext(ctx,
			"SELECT pending_cleanup FROM images WHERE id = ?", id,
		).Scan(&p); err != nil {
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
	createImage(t, q, "idemimg001", createBlob(t, q, "k"), sql.NullString{})
	for _, key := range []string{"stale", "fresh"} {
		if err := q.CreateIdempotencyKey(ctx, sqlc.CreateIdempotencyKeyParams{
			UserID: sqlc.DefaultUserID, Key: key, ImageID: "idemimg001",
		}); err != nil {
			t.Fatalf("建幂等键 %s: %v", key, err)
		}
	}
	old := sqlc.TimeText(time.Now().Add(-48 * time.Hour))
	if _, err := conn.ExecContext(ctx,
		"UPDATE idempotency_keys SET created_at = ? WHERE key = 'stale'", old); err != nil {
		t.Fatalf("回拨创建时间: %v", err)
	}

	sw := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler),
	})
	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}
	if _, err := q.GetIdempotencyImage(ctx,
		sqlc.GetIdempotencyImageParams{UserID: sqlc.DefaultUserID, Key: "stale"},
	); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("超期幂等键未回收: err=%v", err)
	}
	if _, err := q.GetIdempotencyImage(ctx,
		sqlc.GetIdempotencyImageParams{UserID: sqlc.DefaultUserID, Key: "fresh"}); err != nil {
		t.Errorf("期内幂等键被误删: %v", err)
	}
}

// seedAuditRows 写两条审计日志,其中 stale 的记录时刻回拨到 40 天前。
func seedAuditRows(t *testing.T, conn *sql.DB, q *sqlc.Queries) {
	t.Helper()
	ctx := t.Context()
	for _, target := range []string{"stale", "fresh"} {
		if err := q.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{
			Actor: sqlc.DefaultUserID, Action: "channel.create", Target: target,
		}); err != nil {
			t.Fatalf("写审计日志 %s: %v", target, err)
		}
	}
	old := sqlc.TimeText(time.Now().Add(-40 * 24 * time.Hour))
	if _, err := conn.ExecContext(ctx, "UPDATE audit_log SET created_at = ? WHERE target = 'stale'", old); err != nil {
		t.Fatalf("回拨记录时刻: %v", err)
	}
}

// auditTargets 读出审计日志里现存的目标标识集合。
func auditTargets(t *testing.T, q *sqlc.Queries) map[string]bool {
	t.Helper()
	rows, err := q.ListAuditLog(t.Context(), sqlc.ListAuditLogParams{
		Actor: "", Action: "", Target: "", Since: "", Until: "", PageLimit: 10,
	})
	if err != nil {
		t.Fatalf("读审计日志: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Target] = true
	}
	return got
}

// TestSweepExpiresAuditLog 验证审计日志按保留天数回收:超期行删除、期内行保留;保留天数为 0 时一律保留。
func TestSweepExpiresAuditLog(t *testing.T) {
	conn, q := setup(t)
	seedAuditRows(t, conn, q)

	keep := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Settings: testenv.Settings(nil), Logger: slog.New(slog.DiscardHandler),
	})
	if err := keep.SweepOnce(t.Context()); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}
	if got := auditTargets(t, q); !got["stale"] || !got["fresh"] {
		t.Fatalf("保留天数为 0 仍删了审计日志: %v", got)
	}

	trim := lifecycle.New(lifecycle.Config{
		Conn: conn, Queries: q, Logger: slog.New(slog.DiscardHandler),
		Settings: testenv.Settings(func(s *config.Settings) { s.AuditRetentionDays = 30 }),
	})
	if err := trim.SweepOnce(t.Context()); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}
	if got := auditTargets(t, q); got["stale"] || !got["fresh"] {
		t.Fatalf("按 30 天保留后审计日志 = %v, 期望只剩期内的 fresh", got)
	}
}

// TestPhysicalDeleteRefusesReferencedBlob 验证物理删在事务内复查引用计数:
// 扫描到的待删 blob 若又有了引用,删除放弃、副本记录保留、渠道对象不入队删除。
func TestPhysicalDeleteRefusesReferencedBlob(t *testing.T) {
	env := newSweeperEnv(t, nil)
	env.start(t)
	env.putObject(t, "revived")
	ctx := t.Context()
	blobID := createBlob(t, env.Queries, "revived")
	// 待删标记在、宽限已过,但 ref_count 仍为 1(被重新引用)。
	past := sqlc.TimeText(time.Now().Add(-time.Hour))
	if _, err := env.Conn.ExecContext(ctx,
		"UPDATE blobs SET pending_delete = 1, delete_after = ? WHERE id = ?", past, blobID); err != nil {
		t.Fatalf("置待删标记: %v", err)
	}
	markReplicaDone(t, env.Queries, blobID, env.local.ID)

	if err := env.sweeper(testenv.Settings(nil)).SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce 返回错误: %v", err)
	}
	if _, exists := readBlob(t, env.Conn, blobID); !exists {
		t.Fatal("仍被引用的物理图被物理删")
	}
	replica := sqlc.GetBlobChannelParams{BlobID: blobID, ChannelID: env.local.ID}
	if _, err := env.Queries.GetBlobChannel(ctx, replica); err != nil {
		t.Fatalf("放弃物理删后副本记录应随事务回滚保留: %v", err)
	}
	if env.jobCount(t, replication.DeleteObjectArgs{}.Kind()) != 0 || !env.objectExists(t, "revived") {
		t.Fatal("放弃物理删不应入队渠道对象删除")
	}
}
