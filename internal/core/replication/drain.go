package replication

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/store/driver"
)

// drainBatch 排空任务每批取出的副本行数;逐行删对象再删行,批与批之间不留状态。
const drainBatch = 200

// drainJobTimeout 单次排空执行上限;超时中断后交 River 重试,已删的行不回退,从剩余行继续。
const drainJobTimeout = time.Hour

// DrainChannelArgs 排空一个清理中渠道的任务参数:删除其上全部对象,清完删渠道记录。
type DrainChannelArgs struct {
	ChannelID int64 `json:"channel_id"` // 目标渠道主键
}

// Kind 任务类型标识(River 要求)。
func (DrainChannelArgs) Kind() string { return "drain_channel" }

// InsertOpts 按参数去重:同一渠道不重复入队。
func (DrainChannelArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// EnqueueChannelDrain 入队一个渠道的排空任务(幂等,按参数去重)。
// @Description: 渠道须已打清理中标记;入队失败不影响标记,定期对账会为全部清理中渠道补入队。
// @param channelID 目标渠道主键
// @return 入队失败时的错误
func (r *Runner) EnqueueChannelDrain(ctx context.Context, channelID int64) error {
	if _, err := r.client.Insert(ctx, DrainChannelArgs{ChannelID: channelID}, nil); err != nil {
		return fmt.Errorf("入队渠道 %d 排空任务: %w", channelID, err)
	}
	return nil
}

// drainChannelWorker 清理中渠道的排空执行器。
type drainChannelWorker struct {
	river.WorkerDefaults[DrainChannelArgs]
	cfg RunnerConfig // Runner 依赖(共享)
}

// Timeout 单任务执行超时。
func (*drainChannelWorker) Timeout(*river.Job[DrainChannelArgs]) time.Duration {
	return drainJobTimeout
}

// Work 删除渠道上全部副本对象并删除渠道记录。
// @Description: 逐行"删对象 → 删副本行",进度持久在业务表,中断重试从剩余行继续;对象已不存在
// 视为已删。任一对象删不掉即整体报错交 River 退避重试;最后一次尝试仍失败则把原因写进渠道的
// last_error,渠道停在清理中状态,管理员可改为直接移除(保留远端对象)。渠道不在清理中
// (已被直接移除)时任务作废。
func (w *drainChannelWorker) Work(ctx context.Context, job *river.Job[DrainChannelArgs]) error {
	h, ok := w.cfg.Manager.Get(job.Args.ChannelID)
	if !ok || !h.PendingDelete {
		return river.JobCancel(fmt.Errorf("渠道 %d 不在清理中", job.Args.ChannelID))
	}
	deleted, err := w.drain(ctx, h.ID, h.Driver)
	if err != nil {
		if job.Attempt >= job.MaxAttempts {
			w.recordFailure(ctx, h.ID, err)
		}
		return fmt.Errorf("排空渠道 %q: %w", h.Name, err)
	}
	if err := w.cfg.Manager.Remove(ctx, h.ID); err != nil {
		return fmt.Errorf("渠道 %q 对象已清完,删除记录失败: %w", h.Name, err)
	}
	w.cfg.Logger.Info("渠道已清理完毕并删除", "channel", h.Name, "objects", deleted)
	return nil
}

// drain 逐批删除渠道上的副本对象与副本行,直到没有副本行。
// @param channelID 渠道主键
// @param d 渠道驱动
// @return 本次删除的对象数
// @return 列举、删对象或删行失败时的错误
func (w *drainChannelWorker) drain(ctx context.Context, channelID int64, d driver.Driver) (int, error) {
	q := w.cfg.Queries
	deleted := 0
	for {
		rows, err := q.ListBlobChannelsForChannelPage(ctx, sqlc.ListBlobChannelsForChannelPageParams{
			ChannelID: channelID, Limit: drainBatch,
		})
		if err != nil {
			return deleted, fmt.Errorf("列举副本记录: %w", err)
		}
		if len(rows) == 0 {
			return deleted, nil
		}
		for _, row := range rows {
			if err := d.Delete(ctx, row.StorageKey); err != nil && !errors.Is(err, driver.ErrNotFound) {
				return deleted, fmt.Errorf("删除对象 %q: %w", row.StorageKey, err)
			}
			if err := q.DeleteBlobChannel(ctx, sqlc.DeleteBlobChannelParams{
				BlobID: row.BlobID, ChannelID: channelID,
			}); err != nil {
				return deleted, fmt.Errorf("删除副本记录(blob %d): %w", row.BlobID, err)
			}
			deleted++
		}
	}
}

// recordFailure 把排空的最终失败原因写进渠道的探测结果栏,渠道列表可见;写入失败只记录。
// @param channelID 渠道主键
// @param cause 失败原因
func (w *drainChannelWorker) recordFailure(ctx context.Context, channelID int64, cause error) {
	msg := "清理渠道对象失败,已停止重试;可改为直接移除(保留远端对象): " + cause.Error()
	if err := w.cfg.Queries.UpdateChannelProbeResult(context.WithoutCancel(ctx), sqlc.UpdateChannelProbeResultParams{
		Healthy: 0, LastError: sql.NullString{String: msg, Valid: true}, ID: channelID,
	}); err != nil {
		w.cfg.Logger.Error("写回渠道清理失败原因失败", "channel_id", channelID, "err", err)
	}
}
