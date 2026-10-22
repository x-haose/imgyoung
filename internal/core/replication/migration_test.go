package replication_test

import (
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// jobQueues 按目标 blob 返回 River 里补齐任务所在的队列。
func jobQueues(t *testing.T, env *testEnv) map[int64]string {
	t.Helper()
	rows, err := env.conn.QueryContext(t.Context(), `SELECT CAST(json_extract(args, '$.blob_id') AS INTEGER), queue
FROM river_job WHERE kind = 'replicate_blob'`)
	if err != nil {
		t.Fatalf("查询任务表: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]string{}
	for rows.Next() {
		var blobID int64
		var queue string
		if err := rows.Scan(&blobID, &queue); err != nil {
			t.Fatalf("读取任务行: %v", err)
		}
		out[blobID] = queue
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历任务行: %v", err)
	}
	return out
}

// TestBackfillRunsInSeparateQueue 对账重新排队的补课任务进补课队列,与上传时入队的补齐所在的默认队列隔离;
// 同一个 (blob, 渠道) 已在队列里时不会因为队列不同重复入队。
func TestBackfillRunsInSeparateQueue(t *testing.T) {
	env := newEnv(t, false)
	backfill, _ := seedNamedBlob(t, env, "seed_backfill", "hash-backfill")
	live, _ := seedNamedBlob(t, env, "seed_live", "hash-live")

	if err := env.runner.EnqueueReplication(t.Context(), live, []int64{env.backup.ID}); err != nil {
		t.Fatalf("入队上传补齐: %v", err)
	}
	if err := env.runner.Reconcile(t.Context()); err != nil {
		t.Fatalf("对账: %v", err)
	}

	got := jobQueues(t, env)
	if len(got) != 2 {
		t.Fatalf("任务数 = %d, 期望 2(live 已在队列,对账不重复入队): %v", len(got), got)
	}
	if got[live] != "default" {
		t.Errorf("上传补齐所在队列 = %q, 期望 default", got[live])
	}
	if got[backfill] != "backfill" {
		t.Errorf("补课任务所在队列 = %q, 期望 backfill", got[backfill])
	}
}

// TestEnqueueBackfillPagesAndDedups 为指定副本入队补课任务:超过一页(reconcileBatch)也一个不少,全部进补课队列;
// 重复调用按参数去重,不产生重复任务。
func TestEnqueueBackfillPagesAndDedups(t *testing.T) {
	env := newEnv(t, false)
	ids := make([]int64, 1005)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	for range 2 {
		if err := env.runner.EnqueueBackfill(t.Context(), env.backup.ID, ids); err != nil {
			t.Fatalf("入队: %v", err)
		}
	}
	var total, backfill int64
	row := env.conn.QueryRowContext(t.Context(), `SELECT COUNT(*),
COALESCE(SUM(CASE WHEN queue = 'backfill' AND json_extract(args, '$.channel_id') = ? THEN 1 ELSE 0 END), 0)
FROM river_job WHERE kind = 'replicate_blob'`, env.backup.ID)
	if err := row.Scan(&total, &backfill); err != nil || total != 1005 || backfill != 1005 {
		t.Fatalf("任务数 %d、补课队列里该渠道的 %d, err=%v, 期望都是 1005(跨页不漏、重复不增)", total, backfill, err)
	}
}

// TestChannelReplicaListAndRetry 渠道副本清单:按状态过滤(pending 含补齐中)、分页计数与失败原因;
// 立即重试只动这个渠道上失败的副本,返回改回的数量。
func TestChannelReplicaListAndRetry(t *testing.T) {
	env := newEnv(t, false)
	failed, _ := seedNamedBlob(t, env, "seed_failed", "hash-failed")
	uploading, _ := seedNamedBlob(t, env, "seed_uploading", "hash-uploading")
	if _, err := env.conn.ExecContext(t.Context(),
		`INSERT INTO images (id, blob_id, user_id, created_at) VALUES
('img0000001', ?, 'default', '2026-01-02 00:00:00'), ('img0000002', ?, 'default', '2026-01-01 00:00:00')`,
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
		rows[0].ImageID != "img0000002" || rows[0].ImageCount != 2 {
		t.Fatalf("失败清单 = %+v, 期望只有失败的那个 blob,带创建最早的图片(不是 ID 字典序最小的)与引用数 2", rows)
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

	retried, err := env.queries.RetryFailedBlobChannels(t.Context(), env.backup.ID)
	if err != nil || len(retried) != 1 || retried[0] != failed {
		t.Fatalf("重试 = %v, err=%v, 期望改回失败的那个 blob", retried, err)
	}
	if rows := list("failed"); len(rows) != 0 {
		t.Fatalf("重试后仍有失败副本: %+v", rows)
	}
	if other, err := env.queries.RetryFailedBlobChannels(t.Context(), env.local.ID); err != nil || len(other) != 0 {
		t.Fatalf("别的渠道没有失败副本,重试 = %v, err=%v", other, err)
	}
}
