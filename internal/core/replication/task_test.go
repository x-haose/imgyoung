package replication_test

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/replication"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/store/driver"
)

// testEnv 补齐任务测试环境:两个 local 渠道 + 已启动的 River Runner。
type testEnv struct {
	runner  *replication.Runner
	conn    *sql.DB // 写句柄,直查 River 任务表用
	queries *sqlc.Queries
	mgr     *channel.Manager
	dataDir string
	local   channel.Handle // 内置 local(低优先级)
	backup  channel.Handle // 新增渠道(高优先级)
}

// newTestEnv 组装环境并启动 Runner。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := testenv.New(t)
	env.AddLocalChannel(t, "backup", 200, false)

	runner, err := replication.NewRunner(replication.RunnerConfig{
		Conn: env.Conn, Queries: env.Queries, Manager: env.Manager, Logger: env.Logger,
	})
	if err != nil {
		t.Fatalf("NewRunner 返回错误: %v", err)
	}
	if err := runner.Start(t.Context()); err != nil {
		t.Fatalf("启动 Runner: %v", err)
	}
	t.Cleanup(func() {
		if serr := runner.Stop(t.Context()); serr != nil {
			t.Errorf("停止 Runner: %v", serr)
		}
	})

	te := &testEnv{runner: runner, conn: env.Conn, queries: env.Queries, mgr: env.Manager, dataDir: env.DataDir}
	for _, h := range env.Manager.All() {
		if h.Name == "local" {
			te.local = h
		} else {
			te.backup = h
		}
	}
	return te
}

// seedBlob 造一个 blob:对象存在于 local 渠道(done),backup 渠道 pending。
// @return blob 主键与对象键
func seedBlob(t *testing.T, env *testEnv) (int64, string) {
	t.Helper()
	const key = "seed_00000000"
	payload := []byte("replicate-me-0123456789")
	if err := env.local.Driver.Put(t.Context(), key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("写源对象: %v", err)
	}
	blobID, err := env.queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "seedhash", Phash: 1, StorageKey: key, Mime: "image/png",
		Size: int64(len(payload)), Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob 记录: %v", err)
	}
	mustUpsert := func(chID int64, state string) {
		t.Helper()
		if err := env.queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: chID, State: state,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	mustUpsert(env.local.ID, channel.StateDone)
	mustUpsert(env.backup.ID, channel.StatePending)
	return blobID, key
}

// waitUntil 轮询直到条件满足,超时即失败;后台 worker 的进度只能这样观察。
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待%s超时(%s)", what, timeout)
}

// waitDone 等待副本进入 done 状态。
func waitDone(t *testing.T, env *testEnv, blobID, chID int64) {
	t.Helper()
	waitUntil(t, 15*time.Second, "副本就绪", func() bool {
		row, err := env.queries.GetBlobChannel(t.Context(), sqlc.GetBlobChannelParams{
			BlobID: blobID, ChannelID: chID,
		})
		return err == nil && row.State == channel.StateDone
	})
}

// gcResultFor 从一轮对账结果里取指定渠道的统计;缺失即失败。
func gcResultFor(t *testing.T, results []replication.GCResult, name string) replication.GCResult {
	t.Helper()
	for _, r := range results {
		if r.Channel == name {
			return r
		}
	}
	t.Fatalf("对账结果缺少渠道 %q: %+v", name, results)
	return replication.GCResult{}
}

// TestEnqueueReplicationCopiesBlob 验证补齐链路:入队 → worker 复制 → 状态 done,
// 目标渠道可读回同样字节。
func TestEnqueueReplicationCopiesBlob(t *testing.T) {
	env := newTestEnv(t)
	blobID, key := seedBlob(t, env)

	if err := env.runner.EnqueueReplication(t.Context(), blobID, []int64{env.backup.ID}); err != nil {
		t.Fatalf("入队补齐任务: %v", err)
	}
	waitDone(t, env, blobID, env.backup.ID)

	rc, err := env.backup.Driver.Open(t.Context(), key)
	if err != nil {
		t.Fatalf("目标渠道读回: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("关闭对象流: %v", cerr)
	}
	if err != nil || !bytes.Equal(got, []byte("replicate-me-0123456789")) {
		t.Fatalf("副本内容不一致: err=%v", err)
	}
}

// TestReconcileRequeuesPending 验证启动恢复扫描:pending 行无需显式入队也会被补齐。
func TestReconcileRequeuesPending(t *testing.T) {
	env := newTestEnv(t)
	blobID, _ := seedBlob(t, env)

	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("恢复扫描: %v", err)
	}
	waitDone(t, env, blobID, env.backup.ID)
}

// TestReconcileSeedsMissingRows 验证对账收敛播种:blob 在某渠道完全缺
// blob_channels 行(上传快照与渠道创建竞窗的产物)时,对账补上该行并
// 经既有补齐链路收敛为真实副本。
func TestReconcileSeedsMissingRows(t *testing.T) {
	env := newTestEnv(t)
	const key = "race_00000000"
	payload := []byte("seed-race-0123456789")
	if err := env.local.Driver.Put(t.Context(), key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("写源对象: %v", err)
	}
	blobID, err := env.queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "racehash", Phash: 1, StorageKey: key, Mime: "image/png",
		Size: int64(len(payload)), Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob 记录: %v", err)
	}
	// 只落 local 的 done 行,backup 渠道无任何行——模拟竞窗漏播。
	if err := env.queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: env.local.ID, State: channel.StateDone,
	}); err != nil {
		t.Fatalf("写源副本状态: %v", err)
	}

	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("对账扫描: %v", err)
	}
	waitDone(t, env, blobID, env.backup.ID)
	if _, err := env.backup.Driver.Stat(t.Context(), key); err != nil {
		t.Fatalf("补种副本未真实写入: %v", err)
	}
}

// TestReconcileResetsFailedOnHealthyChannel 验证对账把健康渠道上的 failed 副本
// 复位为 pending 并最终补齐(failed 不是永久终态)。
func TestReconcileResetsFailedOnHealthyChannel(t *testing.T) {
	env := newTestEnv(t)
	blobID, _ := seedBlob(t, env)
	if err := env.queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: env.backup.ID, State: channel.StateFailed,
	}); err != nil {
		t.Fatalf("标记副本失败态: %v", err)
	}

	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("对账扫描: %v", err)
	}
	waitDone(t, env, blobID, env.backup.ID)
}

// TestBackfillNewChannel 验证新渠道存量回填:创建渠道即播种 pending 副本行,
// 唤醒补齐管道(Reconcile)后由既有链路消化为真实副本。
func TestBackfillNewChannel(t *testing.T) {
	env := newTestEnv(t)
	blobID, key := seedBlob(t, env)

	thirdID, err := env.mgr.Create(t.Context(), channel.CreateParams{
		Name: "third", Driver: "local",
		Config:   map[string]string{"path": t.TempDir()},
		Priority: 50,
	})
	if err != nil {
		t.Fatalf("创建第三渠道: %v", err)
	}
	row, err := env.queries.GetBlobChannel(t.Context(), sqlc.GetBlobChannelParams{
		BlobID: blobID, ChannelID: thirdID,
	})
	if err != nil {
		t.Fatalf("存量 blob 未随创建播种: %v", err)
	}
	if row.State != channel.StatePending {
		t.Fatalf("播种状态 = %q, 期望 pending", row.State)
	}

	// 唤醒补齐管道(与 API 层创建渠道后的主动唤醒同一入口)。
	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("唤醒补齐: %v", err)
	}
	waitDone(t, env, blobID, thirdID)

	third, ok := env.mgr.Get(thirdID)
	if !ok {
		t.Fatal("第三渠道句柄缺失")
	}
	if _, err := third.Driver.Stat(t.Context(), key); err != nil {
		t.Fatalf("回填副本未真实写入: %v", err)
	}
}

// TestGCRemovesExpiredOrphans 验证孤儿回收:超宽限期的未知对象被删,
// 新鲜对象与系统目录对象豁免。
func TestGCRemovesExpiredOrphans(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	put := func(key string) {
		t.Helper()
		payload := []byte("orphan")
		if err := env.local.Driver.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
			t.Fatalf("写入对象 %s: %v", key, err)
		}
	}
	put("stale-orphan.bin")
	put("fresh-orphan.bin")
	put("_imgyoung/backup/keep.db")
	// 把 stale 对象的修改时间拨回宽限期之外(local 后端以文件 mtime 为准)。
	stalePath := localObjectPath(t, env, "stale-orphan.bin")
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatalf("回拨对象时间: %v", err)
	}

	results, err := env.runner.RunGC(ctx)
	if err != nil {
		t.Fatalf("RunGC 返回错误: %v", err)
	}
	// 盘点口径:两个业务对象各 6 字节,系统目录对象不计;回收 1 个孤儿,无幻影。
	got := gcResultFor(t, results, "local")
	want := replication.GCResult{Channel: "local", Objects: 2, Bytes: 12, OrphansDeleted: 1}
	if got != want {
		t.Errorf("local 对账统计 = %+v, 期望 %+v", got, want)
	}

	if _, err := env.local.Driver.Stat(ctx, "stale-orphan.bin"); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("过期孤儿未被回收: %v", err)
	}
	if _, err := env.local.Driver.Stat(ctx, "fresh-orphan.bin"); err != nil {
		t.Errorf("宽限期内对象被误删: %v", err)
	}
	if _, err := env.local.Driver.Stat(ctx, "_imgyoung/backup/keep.db"); err != nil {
		t.Errorf("系统目录对象被误删: %v", err)
	}
}

// TestGCRecoversPhantom 验证幻影恢复:DB 记为就绪但渠道缺失的副本被
// 回退并重新补齐。
func TestGCRecoversPhantom(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	blobID, key := seedBlob(t, env)
	// 把 backup 渠道标成就绪,但从不真正写入对象 → 幻影。
	if err := env.queries.UpsertBlobChannel(ctx, sqlc.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: env.backup.ID, State: channel.StateDone,
	}); err != nil {
		t.Fatalf("伪造幻影副本: %v", err)
	}

	results, err := env.runner.RunGC(ctx)
	if err != nil {
		t.Fatalf("RunGC 返回错误: %v", err)
	}
	// backup 上一个对象都没有,只有一条幻影被复位;local 的源对象有 done 行,既非孤儿也非幻影。
	wantBackup := replication.GCResult{Channel: "backup", PhantomsRequeued: 1}
	if got := gcResultFor(t, results, "backup"); got != wantBackup {
		t.Errorf("backup 对账统计 = %+v, 期望 %+v", got, wantBackup)
	}
	local := gcResultFor(t, results, "local")
	if local.PhantomsRequeued != 0 || local.OrphansDeleted != 0 || local.Objects != 1 {
		t.Errorf("local 对账统计 = %+v, 期望 1 个对象、无回收无复位", local)
	}
	waitDone(t, env, blobID, env.backup.ID)
	if _, err := env.backup.Driver.Stat(ctx, key); err != nil {
		t.Fatalf("幻影副本未被真实补齐: %v", err)
	}
}

// TestGCRequeuesPhantomAfterCompletedJob 验证补齐任务完成后对象再次丢失,对账能立即重新入队补齐:
// 完成态任务不参与入队去重,否则同一(blob, channel)在完成态任务被清理前都插不进新任务。
func TestGCRequeuesPhantomAfterCompletedJob(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	blobID, key := seedBlob(t, env)
	if err := env.runner.EnqueueReplication(ctx, blobID, []int64{env.backup.ID}); err != nil {
		t.Fatalf("入队补齐任务: %v", err)
	}
	waitDone(t, env, blobID, env.backup.ID)
	// worker 先写 done 再返回,任务进入完成态略晚于副本就绪。
	waitUntil(t, 15*time.Second, "补齐任务进入完成态", func() bool {
		var n int
		err := env.conn.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM river_job WHERE kind = ? AND state = 'completed'", replication.ReplicateArgs{}.Kind(),
		).Scan(&n)
		return err == nil && n == 1
	})
	if err := env.backup.Driver.Delete(ctx, key); err != nil {
		t.Fatalf("制造幻影(删掉备份渠道上的对象): %v", err)
	}

	results, err := env.runner.RunGC(ctx)
	if err != nil {
		t.Fatalf("RunGC 返回错误: %v", err)
	}
	if got := gcResultFor(t, results, "backup"); got.PhantomsRequeued != 1 {
		t.Fatalf("backup 对账统计 = %+v, 期望复位 1 条幻影", got)
	}
	waitDone(t, env, blobID, env.backup.ID)
	if _, err := env.backup.Driver.Stat(ctx, key); err != nil {
		t.Fatalf("完成态任务挡住了重新入队,幻影未被补齐: %v", err)
	}
}

// localObjectPath 计算 local 渠道内对象的磁盘路径(内置渠道位于 dataDir/blobs)。
func localObjectPath(t *testing.T, env *testEnv, key string) string {
	t.Helper()
	return filepath.Join(env.dataDir, "blobs", key)
}

// TestDrainChannelDeletesObjectsAndRecord 验证渠道排空:带清理删除后排空任务删掉渠道上的
// 对象与副本记录,最后删除渠道本身;另一渠道上的副本不受影响。
func TestDrainChannelDeletesObjectsAndRecord(t *testing.T) {
	env := newTestEnv(t)
	blobID, key := seedBlob(t, env)
	if err := env.runner.EnqueueReplication(t.Context(), blobID, []int64{env.backup.ID}); err != nil {
		t.Fatalf("入队补齐任务: %v", err)
	}
	waitDone(t, env, blobID, env.backup.ID)

	if err := env.mgr.Delete(t.Context(), env.backup.ID, true); err != nil {
		t.Fatalf("带清理删除渠道: %v", err)
	}
	if err := env.runner.EnqueueChannelDrain(t.Context(), env.backup.ID); err != nil {
		t.Fatalf("入队排空任务: %v", err)
	}
	waitUntil(t, 15*time.Second, "排空完成", func() bool {
		_, ok := env.mgr.Get(env.backup.ID)
		return !ok
	})
	if _, err := env.backup.Driver.Stat(t.Context(), key); !errors.Is(err, driver.ErrNotFound) {
		t.Fatalf("排空后对象仍在被删渠道上: %v", err)
	}
	if _, err := env.local.Driver.Stat(t.Context(), key); err != nil {
		t.Fatalf("另一渠道的副本被波及: %v", err)
	}
	if _, err := env.queries.GetChannel(t.Context(), env.backup.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("排空后渠道记录仍在: %v", err)
	}
}
