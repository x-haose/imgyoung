package replication

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// workerEnv 直接驱动补齐 worker 的装配:内置 local 作源渠道,backup 作目标渠道;不启动 River,
// 任务行由用例构造,尝试次数与上限由用例指定。
type workerEnv struct {
	env       *testenv.Env
	worker    *replicateWorker
	local     channel.Handle
	backup    channel.Handle
	backupDir string
}

// newWorkerEnv 建两个 local 渠道并装配 worker。
func newWorkerEnv(t *testing.T) *workerEnv {
	t.Helper()
	env := testenv.New(t)
	_, backupDir := env.AddLocalChannel(t, "backup", 200, false)
	we := &workerEnv{env: env, backupDir: backupDir}
	we.worker = &replicateWorker{cfg: RunnerConfig{
		Conn: env.Conn, Queries: env.Queries, Manager: env.Manager, Logger: env.Logger,
	}}
	for _, h := range env.Manager.All() {
		if h.Name == "local" {
			we.local = h
		} else {
			we.backup = h
		}
	}
	return we
}

// seedReplica 造一条 blob 记录:local 渠道记为 done,目标渠道记为 pending。
// @param withSource 为 true 时在 local 真写源对象;为 false 则源缺失,复制必然失败
// @return 任务参数
func (we *workerEnv) seedReplica(t *testing.T, withSource bool) ReplicateArgs {
	t.Helper()
	const key = "seed_00000000"
	payload := []byte("replicate-me-0123456789")
	if withSource {
		if err := we.local.Driver.Put(t.Context(), key, bytes.NewReader(payload), int64(len(payload))); err != nil {
			t.Fatalf("写源对象: %v", err)
		}
	}
	blobID, err := we.env.Queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "seedhash", Phash: 1, StorageKey: key, Mime: "image/png",
		Size: int64(len(payload)), Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob 记录: %v", err)
	}
	for chID, state := range map[int64]string{we.local.ID: channel.StateDone, we.backup.ID: channel.StatePending} {
		if err := we.env.Queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: chID, State: state,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	return ReplicateArgs{BlobID: blobID, ChannelID: we.backup.ID}
}

// replica 读回目标渠道的副本行。
func (we *workerEnv) replica(t *testing.T, args ReplicateArgs) sqlc.BlobChannel {
	t.Helper()
	row, err := we.env.Queries.GetBlobChannel(t.Context(), sqlc.GetBlobChannelParams{
		BlobID: args.BlobID, ChannelID: args.ChannelID,
	})
	if err != nil {
		t.Fatalf("读副本行: %v", err)
	}
	return row
}

// jobAt 构造处于第 attempt 次尝试、上限 maxAttempts 的任务。
func jobAt(args ReplicateArgs, attempt, maxAttempts int) *river.Job[ReplicateArgs] {
	return &river.Job[ReplicateArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: maxAttempts},
		Args:   args,
	}
}

// isCancel 报告错误是否要求 River 取消任务。
func isCancel(err error) bool {
	var cancel *river.JobCancelError
	return errors.As(err, &cancel)
}

// TestReplicateWorkerRetryRecordsCause 验证非末次尝试失败:源对象缺失令复制失败,副本回到 pending、
// 尝试计数加一、失败原因写回,错误不是取消——River 会按退避重试。
func TestReplicateWorkerRetryRecordsCause(t *testing.T) {
	we := newWorkerEnv(t)
	args := we.seedReplica(t, false)

	err := we.worker.Work(t.Context(), jobAt(args, 1, 25))
	if err == nil || isCancel(err) {
		t.Fatalf("源缺失应返回可重试错误, 得 %v", err)
	}
	row := we.replica(t, args)
	if row.State != channel.StatePending || row.Attempts != 1 || row.LastError == "" {
		t.Fatalf("失败后副本行 = %+v, 期望 pending、attempts=1、last_error 非空", row)
	}
}

// TestReplicateWorkerFailsOnLastAttempt 验证末次尝试失败:副本置 failed,原因留在 last_error,
// 由后续对账在渠道健康时复位重来。
func TestReplicateWorkerFailsOnLastAttempt(t *testing.T) {
	we := newWorkerEnv(t)
	args := we.seedReplica(t, false)

	if err := we.worker.Work(t.Context(), jobAt(args, 3, 3)); err == nil || isCancel(err) {
		t.Fatalf("末次失败应返回普通错误, 得 %v", err)
	}
	row := we.replica(t, args)
	if row.State != channel.StateFailed || row.Attempts != 1 || row.LastError == "" {
		t.Fatalf("末次失败后副本行 = %+v, 期望 failed、attempts=1、last_error 非空", row)
	}
}

// TestReplicateWorkerPutFailureRecordsCause 验证目标渠道写入失败同样进入重试路径:
// 目标目录只读时 Put 报错,原因写回 last_error,副本回到 pending。
func TestReplicateWorkerPutFailureRecordsCause(t *testing.T) {
	testenv.SkipIfRoot(t)
	we := newWorkerEnv(t)
	args := we.seedReplica(t, true)
	if err := os.Chmod(we.backupDir, 0o500); err != nil { //nolint:gosec // 需保留读与执行位,只去掉写位才能构造可读不可写
		t.Fatalf("置目标目录只读: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(we.backupDir, 0o700); err != nil { //nolint:gosec // 目录需执行位方可进入,0700 已是最小权限
			t.Errorf("恢复目标目录权限: %v", err)
		}
	})

	if err := we.worker.Work(t.Context(), jobAt(args, 1, 25)); err == nil || isCancel(err) {
		t.Fatalf("目标只读应返回可重试错误, 得 %v", err)
	}
	row := we.replica(t, args)
	if row.State != channel.StatePending || row.Attempts != 1 || row.LastError == "" {
		t.Fatalf("写入失败后副本行 = %+v, 期望 pending、attempts=1、last_error 非空", row)
	}
}

// TestReplicateWorkerSucceedsAndIsIdempotent 验证成功路径与幂等:复制后副本 done、目标可读回同样字节;
// 对已 done 的副本再执行一次直接返回,不再计尝试。
func TestReplicateWorkerSucceedsAndIsIdempotent(t *testing.T) {
	we := newWorkerEnv(t)
	args := we.seedReplica(t, true)

	if err := we.worker.Work(t.Context(), jobAt(args, 1, 25)); err != nil {
		t.Fatalf("复制失败: %v", err)
	}
	row := we.replica(t, args)
	if row.State != channel.StateDone || row.Attempts != 1 || row.LastError != "" {
		t.Fatalf("成功后副本行 = %+v, 期望 done、attempts=1、无错误", row)
	}
	if _, err := we.backup.Driver.Stat(t.Context(), "seed_00000000"); err != nil {
		t.Fatalf("目标渠道无副本对象: %v", err)
	}
	if err := we.worker.Work(t.Context(), jobAt(args, 2, 25)); err != nil {
		t.Fatalf("重复执行应直接成功: %v", err)
	}
	if again := we.replica(t, args); again.Attempts != 1 {
		t.Fatalf("已就绪副本再执行不应计尝试, attempts = %d", again.Attempts)
	}
}

// TestReplicateWorkerCancelsWhenTargetChannelMissing 验证目标渠道管理器里已不存在:副本置 failed 并
// 记明原因,任务取消不再重试。渠道行直接写库而不经管理器加载,正是"行在、渠道不在"的形态。
func TestReplicateWorkerCancelsWhenTargetChannelMissing(t *testing.T) {
	we := newWorkerEnv(t)
	args := we.seedReplica(t, true)
	ghostID, err := we.env.Queries.CreateChannel(t.Context(), sqlc.CreateChannelParams{
		Name: "ghost", Driver: "local", Priority: 10,
	})
	if err != nil {
		t.Fatalf("直写渠道行: %v", err)
	}
	if err := we.env.Queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
		BlobID: args.BlobID, ChannelID: ghostID, State: channel.StatePending,
	}); err != nil {
		t.Fatalf("写副本状态: %v", err)
	}
	ghost := ReplicateArgs{BlobID: args.BlobID, ChannelID: ghostID}

	if err := we.worker.Work(t.Context(), jobAt(ghost, 1, 25)); !isCancel(err) {
		t.Fatalf("目标渠道不存在应取消任务, 得 %v", err)
	}
	row := we.replica(t, ghost)
	if row.State != channel.StateFailed || row.LastError == "" || row.Attempts != 0 {
		t.Fatalf("渠道不存在时副本行 = %+v, 期望 failed、有原因、不计尝试", row)
	}
}

// TestReplicateWorkerCancelsWhenReplicaRowGone 验证副本行已不存在(渠道连同副本行被删):任务取消。
func TestReplicateWorkerCancelsWhenReplicaRowGone(t *testing.T) {
	we := newWorkerEnv(t)
	args := we.seedReplica(t, true)
	if err := we.env.Queries.DeleteBlobChannel(t.Context(), sqlc.DeleteBlobChannelParams{
		BlobID: args.BlobID, ChannelID: args.ChannelID,
	}); err != nil {
		t.Fatalf("删副本行: %v", err)
	}
	if err := we.worker.Work(t.Context(), jobAt(args, 1, 25)); !isCancel(err) {
		t.Fatalf("副本行不存在应取消任务, 得 %v", err)
	}
}

// TestReplicateWorkerCancelsWhenTargetDraining 验证目标渠道清理中:任务取消且副本行原样不动,
// 行由排空任务统一删除。
func TestReplicateWorkerCancelsWhenTargetDraining(t *testing.T) {
	we := newWorkerEnv(t)
	args := we.seedReplica(t, true)
	if err := we.env.Manager.Delete(t.Context(), we.backup.ID, true); err != nil {
		t.Fatalf("标记渠道清理中: %v", err)
	}
	if err := we.worker.Work(context.Background(), jobAt(args, 1, 25)); !isCancel(err) {
		t.Fatalf("目标清理中应取消任务, 得 %v", err)
	}
	row := we.replica(t, args)
	if row.State != channel.StatePending || row.Attempts != 0 {
		t.Fatalf("清理中渠道的副本行 = %+v, 期望原样 pending、不计尝试", row)
	}
}
