// Package replication 副本补齐与对账:River 驱动"确保 blob X 在渠道 Y 有副本",
// 并含 blob_channels 双向对账 GC。
// 围栏设计:blob_channels 业务表是唯一真相,River 任务只是幂等触发器——
// 任务表损坏或丢失时,Reconcile 可从业务表完整重建队列。
package replication

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/store/driver"
)

// reconcileBatch 恢复扫描的分页大小(循环取批直到取尽)。
const reconcileBatch = 1000

// ReconcileInterval 定期对账间隔:重置已恢复渠道的 failed 副本并捞回悬空行。
// 导出供配置可见性切面展示当前生效参数。
const ReconcileInterval = 10 * time.Minute

// replicateQueueWorkers 补齐队列的并发 worker 数。
const replicateQueueWorkers = 4

// replicateJobTimeout 单个补齐任务的执行超时(慢渠道不允许无限占用 worker)。
const replicateJobTimeout = 15 * time.Minute

// ReplicateArgs 补齐一份副本的任务参数。
type ReplicateArgs struct {
	BlobID    int64 `json:"blob_id"`    // 物理图主键
	ChannelID int64 `json:"channel_id"` // 目标渠道主键
}

// Kind 任务类型标识(River 要求)。
func (ReplicateArgs) Kind() string { return "replicate_blob" }

// InsertOpts 按参数去重:同一(blob, channel)在途时不重复入队。
// @Description: 完成态不参与去重——副本做完后对象可能再次丢失(幻影),对账复位后须能立即再入队;
// 把完成态算作重复,则完成态任务被 River 清理前(默认 24 小时)同一键都插不进新任务。
func (ReplicateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: inFlightStates()}}
}

// inFlightStates 入队去重只看尚未结束的任务状态;可用、待定、运行中、已排程四态是 River 要求必含的。
// @return 参与去重的状态集
func inFlightStates() []rivertype.JobState {
	return []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
		rivertype.JobStateRunning, rivertype.JobStateScheduled,
	}
}

// DeleteObjectArgs 删除某渠道内一个对象的任务参数(三段式删除的级联执行)。
type DeleteObjectArgs struct {
	ChannelID  int64  `json:"channel_id"`  // 目标渠道主键
	StorageKey string `json:"storage_key"` // 待删对象键
}

// Kind 任务类型标识(River 要求)。
func (DeleteObjectArgs) Kind() string { return "delete_object" }

// InsertOpts 按参数去重:同一(渠道, key)不重复入队。
func (DeleteObjectArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// RunnerConfig Runner 的全部依赖。
type RunnerConfig struct {
	Conn    *sql.DB          // 数据库连接(River 复用同一 SQLite)
	Queries *sqlc.Queries    // sqlc 查询集
	Manager *channel.Manager // 渠道管理器(取源/目标驱动)
	Purger  ObjectPurger     // CDN 缓存清理执行器;nil 表示 purge 未启用,EnqueuePurge 为空操作
	Metrics PutMetrics       // 指标记录口;可为 nil(不记指标)
	Logger  *slog.Logger     // 结构化日志
}

// PutMetrics 向渠道写对象的指标记录口(由 observability.Metrics 实现)。
type PutMetrics interface {
	// RecordChannelPut 记录一次向渠道写对象的成败。
	RecordChannelPut(name string, ok bool)
}

// QueueStats 任务队列的深度:等待执行与正在执行的任务数。
type QueueStats struct {
	Waiting int64 // 可认领、已排程与待重试的任务数
	Running int64 // 正在执行的任务数
}

// Runner 补齐子系统:封装 River client 的生命周期与入队/恢复扫描。
type Runner struct {
	cfg    RunnerConfig           // 依赖
	client *river.Client[*sql.Tx] // River 客户端
}

// NewRunner 创建 Runner:执行 River 自身迁移并注册 worker。
// @param cfg 依赖,字段均必填
// @return 就绪的 Runner(尚未启动)
// @return 迁移或客户端初始化失败时的错误
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	riverDriver := riversqlite.New(cfg.Conn)
	migrator, err := rivermigrate.New(riverDriver, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化 River 迁移器: %w", err)
	}
	if _, err := migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("执行 River 迁移: %w", err)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &replicateWorker{cfg: cfg})
	river.AddWorker(workers, &deleteObjectWorker{cfg: cfg})
	river.AddWorker(workers, &drainChannelWorker{cfg: cfg})
	// 无条件注册:purge 配置关停后,历史遗留的 cdn_purge 任务仍能被认领并
	// 安静消化(Work 内空操作成功),不产生 unknown kind 噪音。
	river.AddWorker(workers, &cdnPurgeWorker{cfg: cfg})
	client, err := river.NewClient(riverDriver, &river.Config{
		Logger: cfg.Logger,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: replicateQueueWorkers},
		},
		// 进程被硬杀时在跑的任务停在运行态,River 的救援器按此时限把它们重排;
		// 时限须大于任一 worker 的执行超时,否则仍在跑的任务会被重复执行。
		RescueStuckJobsAfter: replicateJobTimeout + time.Minute,
		Workers:              workers,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 River 客户端: %w", err)
	}
	return &Runner{cfg: cfg, client: client}, nil
}

// Start 启动 worker、执行一次恢复扫描,并开启定期对账与 GC 循环。
// @Description: ctx 应为独立于请求信号的生命周期上下文,由 Stop 流程收敛。
// @return 启动或首次扫描失败时的错误
func (r *Runner) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("启动 River 客户端: %w", err)
	}
	if err := r.Reconcile(ctx); err != nil {
		return err
	}
	go r.runPeriodic(ctx)
	return nil
}

// runPeriodic 按各自周期执行对账与 GC,直到 ctx 取消;单轮失败只告警,下一轮照常执行。
// @param ctx 生命周期上下文
func (r *Runner) runPeriodic(ctx context.Context) {
	reconcileTicker := time.NewTicker(ReconcileInterval)
	defer reconcileTicker.Stop()
	gcTicker := time.NewTicker(gcInterval)
	defer gcTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileTicker.C:
			if err := r.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.cfg.Logger.Warn("定期对账未完成", "err", err)
			}
		case <-gcTicker.C:
			if _, err := r.RunGC(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.cfg.Logger.Warn("对账 GC 未完成", "err", err)
			}
		}
	}
}

// Stop 优雅停止 worker(等待进行中的任务完成)。
// @return 停止失败时的错误
func (r *Runner) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("停止 River 客户端: %w", err)
	}
	return nil
}

// EnqueueReplication 为若干目标渠道入队补齐任务(按参数去重,幂等)。
// @param blobID 物理图主键
// @param channelIDs 目标渠道列表
// @return 入队失败时的错误(调用方可仅告警——pending 行会被恢复扫描兜底)
func (r *Runner) EnqueueReplication(ctx context.Context, blobID int64, channelIDs []int64) error {
	var errs []error
	for _, chID := range channelIDs {
		if _, err := r.client.Insert(ctx, ReplicateArgs{BlobID: blobID, ChannelID: chID}, nil); err != nil {
			errs = append(errs, fmt.Errorf("渠道 %d: %w", chID, err))
		}
	}
	return errors.Join(errs...)
}

// EnqueueDelete 入队一个渠道对象删除任务(幂等,按参数去重)。
// @param channelID 目标渠道主键
// @param storageKey 待删对象键
// @return 入队失败时的错误(失败即对象暂留,由对账 GC 作为孤儿兜底回收)
func (r *Runner) EnqueueDelete(ctx context.Context, channelID int64, storageKey string) error {
	if _, err := r.client.Insert(ctx, DeleteObjectArgs{ChannelID: channelID, StorageKey: storageKey}, nil); err != nil {
		return fmt.Errorf("入队删除任务(渠道 %d, key %q): %w", channelID, storageKey, err)
	}
	return nil
}

// Reconcile 对账兜底:为清理中渠道补入排空任务,补种缺失的副本行,重置已恢复渠道的 failed 副本,
// 再分页扫描全部未完成副本重新入队(启动恢复与定期对账共用;入队按参数去重,重复无害)。
// @return 任一步失败时的错误
func (r *Runner) Reconcile(ctx context.Context) error {
	if err := r.requeueDrains(ctx); err != nil {
		return err
	}
	if err := r.seedMissingRows(ctx); err != nil {
		return err
	}
	if err := r.resetFailedReplicas(ctx); err != nil {
		return err
	}
	return r.requeuePending(ctx)
}

// requeueDrains 为每个清理中渠道补入排空任务。
// @Description: 清理中由业务表标记驱动:打标记后入队失败或进程崩溃,排空任务在这里补上。
// @return 入队失败时的错误
func (r *Runner) requeueDrains(ctx context.Context) error {
	for _, h := range r.cfg.Manager.Draining() {
		if err := r.EnqueueChannelDrain(ctx, h.ID); err != nil {
			return err
		}
	}
	return nil
}

// seedMissingRows 为每个渠道补种缺失的副本行。
// @Description: 上传以开始时的渠道快照落副本行,与渠道创建并发时新渠道会漏掉在途 blob 的行;
// 按 NOT EXISTS 周期补齐使缺行自愈,已收敛时近似只读。
// @return 补种失败时的错误
func (r *Runner) seedMissingRows(ctx context.Context) error {
	for _, h := range r.cfg.Manager.All() {
		if _, err := r.cfg.Queries.SeedBlobChannelsForChannel(ctx, h.ID); err != nil {
			return fmt.Errorf("补种渠道 %q 缺失副本行: %w", h.Name, err)
		}
	}
	return nil
}

// resetFailedReplicas 把健康渠道上的 failed 副本复位为 pending。
// @Description: failed 不是永久终态,渠道恢复健康后它的失败副本应得到重来的机会。
// @return 复位失败时的错误
func (r *Runner) resetFailedReplicas(ctx context.Context) error {
	for _, h := range r.cfg.Manager.Healthy() {
		if err := r.cfg.Queries.ResetFailedBlobChannels(ctx, h.ID); err != nil {
			return fmt.Errorf("重置渠道 %q 的失败副本: %w", h.Name, err)
		}
	}
	return nil
}

// requeuePending 分页扫描全部未完成副本,逐页批量入队补齐任务。
// @return 扫描或入队失败时的错误
func (r *Runner) requeuePending(ctx context.Context) error {
	total := 0
	for offset := int64(0); ; offset += reconcileBatch {
		rows, err := r.cfg.Queries.ListPendingBlobChannelsPage(ctx, sqlc.ListPendingBlobChannelsPageParams{
			Limit: reconcileBatch, Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("扫描未完成副本: %w", err)
		}
		if err := r.insertReplications(ctx, rows); err != nil {
			return err
		}
		total += len(rows)
		if len(rows) < reconcileBatch {
			break
		}
	}
	if total > 0 {
		r.cfg.Logger.Info("对账扫描完成", "requeued", total)
	}
	return nil
}

// insertReplications 把一页未完成副本在一个事务内批量入队补齐任务;空页为空操作。
// @Description: 入队按参数去重,已在队列中的任务被跳过,重复扫描无害。
// @param rows 一页 (blob, channel) 对
// @return 入队失败时的错误
func (r *Runner) insertReplications(ctx context.Context, rows []sqlc.ListPendingBlobChannelsPageRow) error {
	if len(rows) == 0 {
		return nil
	}
	params := make([]river.InsertManyParams, 0, len(rows))
	for _, row := range rows {
		params = append(params, river.InsertManyParams{
			Args: ReplicateArgs{BlobID: row.BlobID, ChannelID: row.ChannelID},
		})
	}
	if _, err := r.client.InsertMany(ctx, params); err != nil {
		return fmt.Errorf("批量重建 %d 个补齐任务: %w", len(params), err)
	}
	return nil
}

// replicateWorker 单份副本的复制执行器。
type replicateWorker struct {
	river.WorkerDefaults[ReplicateArgs]
	cfg RunnerConfig // Runner 依赖(共享)
}

// Timeout 单任务执行超时(River 到时中断,防慢渠道占死 worker)。
func (*replicateWorker) Timeout(*river.Job[ReplicateArgs]) time.Duration {
	return replicateJobTimeout
}

// Work 执行一次副本复制:从任一就绪渠道读出,写入目标渠道,状态写回业务表。
// @Description: 幂等——副本已 done 直接返回;目标渠道不存在则取消任务并标记 failed;
// 目标渠道清理中则取消任务;复制失败回退 pending 并交由 River 重试,最后一次尝试失败标记 failed。
func (w *replicateWorker) Work(ctx context.Context, job *river.Job[ReplicateArgs]) error {
	args := job.Args
	q := w.cfg.Queries

	current, err := q.GetBlobChannel(ctx, sqlc.GetBlobChannelParams{BlobID: args.BlobID, ChannelID: args.ChannelID})
	if errors.Is(err, sql.ErrNoRows) {
		// 副本记录已不存在(渠道被删),任务作废。
		return river.JobCancel(fmt.Errorf("副本记录不存在: blob %d → 渠道 %d", args.BlobID, args.ChannelID))
	}
	if err != nil {
		return fmt.Errorf("读取副本状态: %w", err)
	}
	if current.State == channel.StateDone {
		return nil
	}

	target, ok := w.cfg.Manager.Get(args.ChannelID)
	if !ok {
		w.markState(ctx, args, channel.StateFailed, "目标渠道不存在", 0)
		return river.JobCancel(fmt.Errorf("目标渠道 %d 不存在", args.ChannelID))
	}
	if target.PendingDelete {
		// 渠道正在清理:往上面补副本只是给排空任务添活,副本行由它一并删除。
		return river.JobCancel(fmt.Errorf("目标渠道 %q 正在清理中", target.Name))
	}
	blob, err := q.GetBlob(ctx, args.BlobID)
	if err != nil {
		return fmt.Errorf("读取 blob %d: %w", args.BlobID, err)
	}

	w.markState(ctx, args, channel.StateUploading, "", 1)
	if err := w.copyBlob(ctx, blob, target); err != nil {
		if job.Attempt >= job.MaxAttempts {
			w.markState(ctx, args, channel.StateFailed, err.Error(), 0)
		} else {
			w.markState(ctx, args, channel.StatePending, err.Error(), 0)
		}
		return fmt.Errorf("复制 blob %d → 渠道 %q: %w", args.BlobID, target.Name, err)
	}
	w.markState(ctx, args, channel.StateDone, "", 0)
	w.cfg.Logger.Info("副本补齐完成", "blob", args.BlobID, "channel", target.Name)
	return nil
}

// copyBlob 从优先级最高的就绪源渠道流式复制到目标渠道。
// @param blob 物理图记录
// @param target 目标渠道
// @return 无可用源或读写失败时的错误
func (w *replicateWorker) copyBlob(ctx context.Context, blob sqlc.Blob, target channel.Handle) error {
	sources, err := w.cfg.Manager.CandidatesForBlob(ctx, blob.ID)
	if err != nil {
		return err
	}
	var lastErr error
	lastErr = errors.New("无可用源副本")
	for _, src := range sources {
		if src.ID == target.ID {
			continue
		}
		var rc io.ReadCloser
		if rc, lastErr = src.Driver.Open(ctx, blob.StorageKey); lastErr != nil {
			continue
		}
		lastErr = target.Driver.Put(ctx, blob.StorageKey, rc, blob.Size)
		if cerr := rc.Close(); cerr != nil && lastErr == nil {
			lastErr = cerr
		}
		if w.cfg.Metrics != nil {
			w.cfg.Metrics.RecordChannelPut(target.Name, lastErr == nil)
		}
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// markState 写回副本状态,失败仅告警(业务表兜底,不阻断任务流转)。
// @param attemptsDelta 尝试计数增量(仅置 uploading 时为 1,保证计数=实际尝试次数)
func (w *replicateWorker) markState(ctx context.Context, args ReplicateArgs, state, lastError string,
	attemptsDelta int64) {
	if err := w.cfg.Queries.MarkBlobChannel(ctx, sqlc.MarkBlobChannelParams{
		State: state, LastError: lastError, Attempts: attemptsDelta,
		BlobID: args.BlobID, ChannelID: args.ChannelID,
	}); err != nil {
		w.cfg.Logger.Error("写回副本状态失败", "blob", args.BlobID, "channel", args.ChannelID, "err", err)
	}
}

// deleteObjectWorker 渠道对象删除执行器(三段式删除的物理级联)。
type deleteObjectWorker struct {
	river.WorkerDefaults[DeleteObjectArgs]
	cfg RunnerConfig // Runner 依赖(共享)
}

// Timeout 单任务执行超时。
func (*deleteObjectWorker) Timeout(*river.Job[DeleteObjectArgs]) time.Duration {
	return replicateJobTimeout
}

// Work 从目标渠道删除一个对象。
// @Description: 幂等——对象已不存在(ErrNotFound)视为删除成功;渠道已被删则任务作废
// (对象随渠道消失或成孤儿由对账 GC 兜底)。其余错误交 River 重试,永久失败后对象
// 成为孤儿,由对账 GC 最终回收。
func (w *deleteObjectWorker) Work(ctx context.Context, job *river.Job[DeleteObjectArgs]) error {
	args := job.Args
	h, ok := w.cfg.Manager.Get(args.ChannelID)
	if !ok {
		return river.JobCancel(fmt.Errorf("目标渠道 %d 不存在", args.ChannelID))
	}
	if err := h.Driver.Delete(ctx, args.StorageKey); err != nil && !errors.Is(err, driver.ErrNotFound) {
		return fmt.Errorf("删除渠道 %q 对象 %q: %w", h.Name, args.StorageKey, err)
	}
	w.cfg.Logger.Info("渠道对象已删除", "channel", h.Name, "key", args.StorageKey)
	return nil
}

// QueueStats 统计任务队列深度,供指标在 scrape 时读取。
// @Description: 直接查 River 的任务表:状态列是 River 的公开契约,本项目不另记一份队列状态。
// @param ctx 控制超时与取消
// @return 队列深度
// @return 查询失败时的错误
func (r *Runner) QueueStats(ctx context.Context) (QueueStats, error) {
	var st QueueStats
	err := r.cfg.Conn.QueryRowContext(ctx, `
SELECT CAST(COALESCE(SUM(CASE WHEN state IN ('available', 'scheduled', 'retryable') THEN 1 ELSE 0 END), 0) AS INTEGER),
       CAST(COALESCE(SUM(CASE WHEN state = 'running' THEN 1 ELSE 0 END), 0) AS INTEGER)
FROM river_job`).Scan(&st.Waiting, &st.Running)
	if err != nil {
		return QueueStats{}, fmt.Errorf("统计任务队列深度: %w", err)
	}
	return st, nil
}
