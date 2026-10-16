package replication

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"
)

// drainBatch 排空任务每批取出的副本行数;逐行删对象再删行,批与批之间不留状态。
const drainBatch = 200

// drainJobTimeout 单次排空执行上限;超时中断后立即重排,已删的行不回退,从剩余行继续。
const drainJobTimeout = time.Hour

// drainFailureReportAttempt 排空任务第几次尝试失败时起才把原因写进渠道、开启告警。
// 按 River 默认退避(第 n 次失败后等 n⁴ 秒),各次尝试都很快失败时,第 4 次尝试在首次失败约 98 秒后开始(超时失败的
// 每次另需 drainJobTimeout);它仍失败才算持续故障,
// 一次网络抖动在前几次重试里就过去了,不留原因也不开告警。
const drainFailureReportAttempt = 4

// DrainChannelArgs 排空一个清理中渠道的任务参数:删除本服务写在其上的对象,清完删渠道记录。
type DrainChannelArgs struct {
	ChannelID int64 `json:"channel_id"` // 目标渠道主键
}

// Kind 任务类型标识(River 要求)。
func (DrainChannelArgs) Kind() string { return "drain_channel" }

// InsertOpts 按参数去重:同一渠道尚未结束的排空任务只有一个;已完成的不算重复,对账可随时补入队。
func (DrainChannelArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: inFlightStates()}}
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

// errStopped 渠道在排空途中已不在清理中(管理员已直接移除),排空就此停下。
var errStopped = errors.New("渠道已不在清理中")

// drainChannelWorker 清理中渠道的排空执行器。
type drainChannelWorker struct {
	river.WorkerDefaults[DrainChannelArgs]
	cfg RunnerConfig // Runner 依赖(共享)
}

// Timeout 单任务执行超时。
func (*drainChannelWorker) Timeout(*river.Job[DrainChannelArgs]) time.Duration {
	return drainJobTimeout
}

// Work 删除本服务写在渠道上的全部对象并删除渠道记录。
// @Description: 先等标记清理中之前开始的写入(上传、补齐、快照、深探测)写完,它们写下的对象与记录这时都已
// 可见;再逐行"删副本对象 → 删副本行",进度持久在业务表,中断重试从剩余行继续;最后删系统目录下的对象(含
// 元数据库快照)与深探测残留,别的程序写的对象不动。每个对象删除前都按渠道表确认渠道仍在清理中:管理员
// 中途直接移除即停下,除了正在删的那一个,剩下的远端对象原样保留。对象已不存在视为已删。任一对象删不掉即整体报错交 River 退避重试;
// 同一任务第 drainFailureReportAttempt 次起仍失败,才把原因写进渠道的失败栏(随之开启 drain_failed 告警)。
// 尝试次数用尽后对账为清理中渠道重新入队,重试不会停止,渠道停在清理中,直到清完或管理员直接移除。被停机打断,
// 或超时而本次删掉了对象时,立即重排、不计尝试次数,也不写原因(见 interrupted)。
func (w *drainChannelWorker) Work(ctx context.Context, job *river.Job[DrainChannelArgs]) error {
	h, ok := w.cfg.Manager.Get(job.Args.ChannelID)
	if !ok || !h.PendingDelete {
		return river.JobCancel(fmt.Errorf("渠道 %d 不在清理中", job.Args.ChannelID))
	}
	w.cfg.Manager.AwaitWrites(h.ID)
	still := func() error { return w.stillDraining(ctx, h.ID) }
	replicas, err := w.drain(ctx, h.ID, h.Driver, still)
	system := 0
	if err == nil {
		system, err = purgeSystemObjects(ctx, h.Driver, still)
	}
	if errors.Is(err, errStopped) {
		w.cfg.Logger.Info("渠道已被直接移除,排空停止,剩余远端对象保留", "channel", h.Name, "objects", replicas)
		return nil
	}
	if err != nil {
		if interrupted(ctx, replicas+system) {
			// 进度已在业务表,下次从剩余部分继续;Snooze 不消耗尝试次数。
			w.cfg.Logger.Info("排空被超时或停机打断,随即从剩余部分继续", "channel", h.Name, "err", err)
			return river.JobSnooze(0)
		}
		if job.Attempt >= drainFailureReportAttempt {
			w.recordFailure(ctx, h.ID, err)
		}
		return fmt.Errorf("排空渠道 %q: %w", h.Name, err)
	}
	removed, err := w.cfg.Manager.Remove(ctx, h.ID)
	if err != nil {
		return fmt.Errorf("渠道 %q 对象已清完,删除记录失败: %w", h.Name, err)
	}
	if removed {
		w.cfg.Logger.Info("渠道已清理完毕并删除", "channel", h.Name, "objects", replicas, "system_objects", system)
	}
	return nil
}

// interrupted 判断一次失败的排空是否只是被打断、应立即接着做而不计为失败。
// @Description: 停机打断恒为是。超时只在本次删掉了对象时为是:一次也推进不了的排空(如单次列举或删除就撞上
// 时限)按失败计,随尝试次数退避,第 drainFailureReportAttempt 次起写出原因并开启告警,而不是无休止地重跑。
// @param ctx 本次执行的上下文
// @param deleted 本次删除的对象数(副本与系统对象合计)
// @return 是否按打断处理
func interrupted(ctx context.Context, deleted int) bool {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return deleted > 0
	}
	return false
}

// stillDraining 按渠道表确认渠道仍在清理中。
// @param channelID 渠道主键
// @return 渠道已删除或已不在清理中时为 errStopped;查询失败时的错误
func (w *drainChannelWorker) stillDraining(ctx context.Context, channelID int64) error {
	row, err := w.cfg.Queries.GetChannel(ctx, channelID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return errStopped
	case err != nil:
		return fmt.Errorf("确认渠道仍在清理中: %w", err)
	case row.PendingDelete == 0:
		return errStopped
	}
	return nil
}

// drain 逐批删除渠道上的副本对象与副本行,直到没有副本行。
// @param channelID 渠道主键
// @param d 渠道驱动
// @param still 确认渠道仍在清理中(见 stillDraining);每个对象删除前调用
// @return 本次删除的对象数
// @return 渠道已不在清理中时为 errStopped;列举、删对象或删行失败时的错误
func (w *drainChannelWorker) drain(ctx context.Context, channelID int64, d driver.Driver,
	still func() error) (int, error) {
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
		n, err := w.deleteReplicas(ctx, channelID, d, rows, still)
		deleted += n
		if err != nil {
			return deleted, err
		}
	}
}

// deleteReplicas 逐行删除一批副本:先删对象(已不存在视为已删),再删副本行。
// @param channelID 渠道主键
// @param d 渠道驱动
// @param rows 一批副本行
// @param still 确认渠道仍在清理中;每个对象删除前调用
// @return 删除的对象数
// @return 渠道已不在清理中时为 errStopped;删对象或删行失败时的错误
func (w *drainChannelWorker) deleteReplicas(ctx context.Context, channelID int64, d driver.Driver,
	rows []sqlc.ListBlobChannelsForChannelPageRow, still func() error) (int, error) {
	for i, row := range rows {
		if err := still(); err != nil {
			return i, err
		}
		if err := d.Delete(ctx, row.StorageKey); err != nil && !errors.Is(err, driver.ErrNotFound) {
			return i, fmt.Errorf("删除对象 %q: %w", row.StorageKey, err)
		}
		if err := w.cfg.Queries.DeleteBlobChannel(ctx, sqlc.DeleteBlobChannelParams{
			BlobID: row.BlobID, ChannelID: channelID,
		}); err != nil {
			return i, fmt.Errorf("删除副本记录(blob %d): %w", row.BlobID, err)
		}
	}
	return len(rows), nil
}

// purgeSystemObjects 删除本服务写在渠道上的系统对象:系统目录下的全部对象与渠道根下的深探测残留。
// @Description: 深探测与图片写在同一位置(渠道根),残留也就在根下。系统目录递归列举,渠道根只列举这一层,
// 都不列举整个渠道:别的程序放在子目录里的对象再多,也不拖长排空。
// @param d 渠道驱动
// @param still 确认渠道仍在清理中;每个对象删除前调用
// @return 删除的对象数
// @return 渠道已不在清理中时为 errStopped;列举或删除失败时的错误
func purgeSystemObjects(ctx context.Context, d driver.Driver, still func() error) (int, error) {
	targets, err := d.List(ctx, systemDir)
	if err != nil {
		return 0, fmt.Errorf("列举系统目录: %w", err)
	}
	top, err := d.ListShallow(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("列举渠道根: %w", err)
	}
	for _, o := range top {
		if failover.IsDeepProbeKey(o.Key) {
			targets = append(targets, o)
		}
	}
	deleted := 0
	for _, o := range targets {
		if err := still(); err != nil {
			return deleted, err
		}
		if err := d.Delete(ctx, o.Key); err != nil && !errors.Is(err, driver.ErrNotFound) {
			return deleted, fmt.Errorf("删除系统对象 %q: %w", o.Key, err)
		}
		deleted++
	}
	return deleted, nil
}

// recordFailure 把排空失败的结论写进渠道的失败栏,渠道列表与告警可见;写入失败只记录。
// @Description: 只写清理中的渠道;此刻已被直接移除的渠道不写。写入失败的后果只是界面与告警缺一条原因,
// 排空本身照样报错重试,故只记日志。
// @param channelID 渠道主键
// @param cause 失败原因
func (w *drainChannelWorker) recordFailure(ctx context.Context, channelID int64, cause error) {
	f := w.cfg.Manager.ExplainFor(channelID, cause)
	params := sqlc.RecordChannelDrainFailureParams{
		LastErrorReason: sql.NullString{String: string(f.Reason), Valid: true},
		LastError:       sql.NullString{String: f.Detail, Valid: true},
		ID:              channelID,
	}
	if _, err := w.cfg.Queries.RecordChannelDrainFailure(context.WithoutCancel(ctx), params); err != nil {
		w.cfg.Logger.Error("写回渠道清理失败原因失败", "channel_id", channelID, "err", err)
	}
}
