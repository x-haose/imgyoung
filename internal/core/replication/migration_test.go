package replication_test

import (
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// jobPriorities 按目标 blob 返回 River 里补齐任务的优先级。
func jobPriorities(t *testing.T, env *testEnv) map[int64]int {
	t.Helper()
	rows, err := env.conn.QueryContext(t.Context(), `SELECT CAST(json_extract(args, '$.blob_id') AS INTEGER), priority
FROM river_job WHERE kind = 'replicate_blob'`)
	if err != nil {
		t.Fatalf("查询任务表: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]int{}
	for rows.Next() {
		var blobID int64
		var priority int
		if err := rows.Scan(&blobID, &priority); err != nil {
			t.Fatalf("读取任务行: %v", err)
		}
		out[blobID] = priority
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历任务行: %v", err)
	}
	return out
}

// TestBackfillQueuedBelowLiveReplication 对账重新排队的补课任务优先级低于上传时入队的补齐,
// 并且同一个 (blob, 渠道) 已在队列里时不会因为优先级不同重复入队。
func TestBackfillQueuedBelowLiveReplication(t *testing.T) {
	env := newEnv(t, false)
	backfill, _ := seedNamedBlob(t, env, "seed_backfill", "hash-backfill")
	live, _ := seedNamedBlob(t, env, "seed_live", "hash-live")

	if err := env.runner.EnqueueReplication(t.Context(), live, []int64{env.backup.ID}); err != nil {
		t.Fatalf("入队上传补齐: %v", err)
	}
	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("对账: %v", err)
	}

	got := jobPriorities(t, env)
	if len(got) != 2 {
		t.Fatalf("任务数 = %d, 期望 2(live 已在队列,对账不重复入队): %v", len(got), got)
	}
	if got[live] != 1 {
		t.Errorf("上传补齐的优先级 = %d, 期望 1(最高)", got[live])
	}
	if got[backfill] <= got[live] {
		t.Errorf("补课任务优先级 = %d, 期望低于上传补齐的 %d", got[backfill], got[live])
	}
}

// TestChannelReplicaListAndRetry 渠道副本清单:按状态过滤(pending 含补齐中)、分页计数与失败原因;
// 立即重试只动这个渠道上失败的副本,返回改回的数量。
func TestChannelReplicaListAndRetry(t *testing.T) {
	env := newEnv(t, false)
	failed, _ := seedNamedBlob(t, env, "seed_failed", "hash-failed")
	uploading, _ := seedNamedBlob(t, env, "seed_uploading", "hash-uploading")
	if _, err := env.conn.ExecContext(t.Context(),
		`INSERT INTO images (id, blob_id, user_id) VALUES ('img0000001', ?, 'default'), ('img0000002', ?, 'default')`,
		failed, failed); err != nil {
		t.Fatalf("造图片引用: %v", err)
	}
	mark := func(blobID int64, state, reason string) {
		t.Helper()
		if err := env.queries.MarkBlobChannel(t.Context(), sqlc.MarkBlobChannelParams{
			State: state, LastErrorReason: reason, LastError: "boom", Attempts: 1,
			BlobID: blobID, ChannelID: env.backup.ID,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	mark(failed, channel.StateFailed, "timeout")
	mark(uploading, channel.StateUploading, "")

	list := func(state string) []sqlc.ListChannelReplicasRow {
		t.Helper()
		rows, err := env.queries.ListChannelReplicas(t.Context(), sqlc.ListChannelReplicasParams{
			ChannelID: env.backup.ID, State: state, PageLimit: 10,
		})
		if err != nil {
			t.Fatalf("列出副本(%q): %v", state, err)
		}
		return rows
	}
	if rows := list("failed"); len(rows) != 1 || rows[0].BlobID != failed || rows[0].LastErrorReason != "timeout" ||
		rows[0].ImageID != "img0000001" || rows[0].ImageCount != 2 {
		t.Fatalf("失败清单 = %+v, 期望只有失败的那个 blob,带最早的图片与引用数 2", rows)
	}
	if rows := list("pending"); len(rows) != 1 || rows[0].BlobID != uploading || rows[0].ImageID != "" {
		t.Fatalf("待补清单 = %+v, 期望含补齐中的 blob,且没有图片引用时 image_id 为空", rows)
	}
	if rows := list(""); len(rows) != 2 {
		t.Fatalf("全部清单 = %d 条, 期望 2", len(rows))
	}
	total, err := env.queries.CountChannelReplicas(t.Context(), sqlc.CountChannelReplicasParams{
		ChannelID: env.backup.ID, State: "failed",
	})
	if err != nil || total != 1 {
		t.Fatalf("失败总数 = %d, err=%v, 期望 1", total, err)
	}

	n, err := env.queries.RetryFailedBlobChannels(t.Context(), env.backup.ID)
	if err != nil || n != 1 {
		t.Fatalf("重试 = %d, err=%v, 期望改回 1 个", n, err)
	}
	if rows := list("failed"); len(rows) != 0 {
		t.Fatalf("重试后仍有失败副本: %+v", rows)
	}
	if n, err = env.queries.RetryFailedBlobChannels(t.Context(), env.local.ID); err != nil || n != 0 {
		t.Fatalf("别的渠道没有失败副本,重试 = %d, err=%v", n, err)
	}
}
