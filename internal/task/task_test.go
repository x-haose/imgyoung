package task_test

import (
	"bytes"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/channel"
	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/secret"
	"github.com/x-haose/imgyoung/internal/task"
)

// testEnv 补齐任务测试环境:两个 local 渠道 + River Runner。
type testEnv struct {
	runner  *task.Runner
	queries *db.Queries
	mgr     *channel.Manager
	local   channel.Handle // 内置 local(低优先级)
	backup  channel.Handle // 新增渠道(高优先级)
}

// newTestEnv 组装环境并启动 Runner。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "test.db"))
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
	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	queries := db.New(conn)
	mgr, err := channel.NewManager(t.Context(), channel.ManagerConfig{
		Conn: conn, Queries: queries, Master: master, DataDir: dir,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("创建渠道管理器: %v", err)
	}
	if _, err := mgr.Create(t.Context(), channel.CreateParams{
		Name: "backup", Driver: "local",
		Config:   map[string]string{"path": t.TempDir()},
		Priority: 200,
	}); err != nil {
		t.Fatalf("创建备份渠道: %v", err)
	}

	runner, err := task.NewRunner(task.RunnerConfig{
		Conn:    conn,
		Queries: queries,
		Manager: mgr,
		Logger:  slog.New(slog.DiscardHandler),
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

	env := &testEnv{runner: runner, queries: queries, mgr: mgr}
	for _, h := range mgr.All() {
		if h.Name == "local" {
			env.local = h
		} else {
			env.backup = h
		}
	}
	return env
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
	blobID, err := env.queries.CreateBlob(t.Context(), db.CreateBlobParams{
		Sha256: "seedhash", Phash: 1, StorageKey: key, Mime: "image/png",
		Size: int64(len(payload)), Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob 记录: %v", err)
	}
	mustUpsert := func(chID int64, state string) {
		t.Helper()
		if err := env.queries.UpsertBlobChannel(t.Context(), db.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: chID, State: state,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	mustUpsert(env.local.ID, channel.StateDone)
	mustUpsert(env.backup.ID, channel.StatePending)
	return blobID, key
}

// waitState 轮询等待副本达到目标状态。
func waitState(t *testing.T, env *testEnv, blobID, chID int64, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		row, err := env.queries.GetBlobChannel(t.Context(), db.GetBlobChannelParams{
			BlobID: blobID, ChannelID: chID,
		})
		if err == nil && row.State == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待副本状态 %q 超时", want)
}

// TestEnqueueReplicationCopiesBlob 验证补齐链路:入队 → worker 复制 → 状态 done,
// 目标渠道可读回同样字节。
func TestEnqueueReplicationCopiesBlob(t *testing.T) {
	env := newTestEnv(t)
	blobID, key := seedBlob(t, env)

	if err := env.runner.EnqueueReplication(t.Context(), blobID, []int64{env.backup.ID}); err != nil {
		t.Fatalf("入队补齐任务: %v", err)
	}
	waitState(t, env, blobID, env.backup.ID, channel.StateDone)

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
	waitState(t, env, blobID, env.backup.ID, channel.StateDone)
}

// TestReconcileResetsFailedOnHealthyChannel 验证对账把健康渠道上的 failed 副本
// 复位为 pending 并最终补齐(failed 不是永久终态)。
func TestReconcileResetsFailedOnHealthyChannel(t *testing.T) {
	env := newTestEnv(t)
	blobID, _ := seedBlob(t, env)
	if err := env.queries.UpsertBlobChannel(t.Context(), db.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: env.backup.ID, State: channel.StateFailed,
	}); err != nil {
		t.Fatalf("标记副本失败态: %v", err)
	}

	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("对账扫描: %v", err)
	}
	waitState(t, env, blobID, env.backup.ID, channel.StateDone)
}
