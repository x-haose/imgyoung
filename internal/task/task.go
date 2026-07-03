// Package task 异步任务执行层:River 驱动的副本补齐(设计定稿·上传管理/数据模型)。
// 围栏设计:blob_channels 业务表是唯一真相,River 任务只是幂等触发器——
// 任务表损坏或丢失时,Reconcile 可从业务表完整重建队列。
package task

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

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db"
)

// reconcileBatch 恢复扫描的分页大小(循环取批直到取尽)。
const reconcileBatch = 1000

// reconcileInterval 定期对账间隔:重置已恢复渠道的 failed 副本并捞回悬空行。
const reconcileInterval = 10 * time.Minute

// replicateQueueWorkers 补齐队列的并发 worker 数。
const replicateQueueWorkers = 4

// replicateJobTimeout 单个补齐任务的执行超时(慢渠道不允许无限占用 worker)。
const replicateJobTimeout = 15 * time.Minute

// ReplicateArgs 补齐一份副本的任务参数。
type ReplicateArgs struct {
	// BlobID 物理图主键。
	BlobID int64 `json:"blob_id"`
	// ChannelID 目标渠道主键。
	ChannelID int64 `json:"channel_id"`
}

// Kind 任务类型标识(River 要求)。
func (ReplicateArgs) Kind() string { return "replicate_blob" }

// InsertOpts 按参数去重:同一(blob, channel)不重复入队。
func (ReplicateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// RunnerConfig Runner 的全部依赖。
type RunnerConfig struct {
	// Conn 数据库连接(River 复用同一 SQLite)。
	Conn *sql.DB
	// Queries sqlc 查询集。
	Queries *db.Queries
	// Manager 渠道管理器(取源/目标驱动)。
	Manager *channel.Manager
	// Logger 结构化日志。
	Logger *slog.Logger
}

// Runner 补齐子系统:封装 River client 的生命周期与入队/恢复扫描。
type Runner struct {
	// cfg 依赖。
	cfg RunnerConfig
	// client River 客户端。
	client *river.Client[*sql.Tx]
}

// NewRunner 创建 Runner:执行 River 自身迁移并注册 worker。
// @param cfg 依赖,字段均必填
// @return 就绪的 Runner(尚未启动)
// @return 迁移或客户端初始化失败时的错误
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	driver := riversqlite.New(cfg.Conn)
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化 River 迁移器: %w", err)
	}
	if _, err := migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("执行 River 迁移: %w", err)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &replicateWorker{cfg: cfg})
	client, err := river.NewClient(driver, &river.Config{
		Logger: cfg.Logger,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: replicateQueueWorkers},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 River 客户端: %w", err)
	}
	return &Runner{cfg: cfg, client: client}, nil
}

// Start 启动 worker、执行一次恢复扫描,并开启定期对账循环。
// @Description: ctx 应为独立于请求信号的生命周期上下文,由 Stop 流程收敛。
// @return 启动或首次扫描失败时的错误
func (r *Runner) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("启动 River 客户端: %w", err)
	}
	if err := r.Reconcile(ctx); err != nil {
		return err
	}
	go func() {
		reconcileTicker := time.NewTicker(reconcileInterval)
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
	}()
	return nil
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

// Reconcile 对账兜底:重置已恢复渠道的 failed 副本,再分页扫描全部未完成
// 副本重新入队(启动恢复与定期对账共用;入队按参数去重,重复无害)。
// @return 扫描或入队失败时的错误
func (r *Runner) Reconcile(ctx context.Context) error {
	// failed 不是永久终态:渠道恢复健康后给它的失败副本重来的机会。
	for _, h := range r.cfg.Manager.Healthy() {
		if err := r.cfg.Queries.ResetFailedBlobChannels(ctx, h.ID); err != nil {
			return fmt.Errorf("重置渠道 %q 的失败副本: %w", h.Name, err)
		}
	}
	total := 0
	for offset := int64(0); ; offset += reconcileBatch {
		rows, err := r.cfg.Queries.ListPendingBlobChannelsPage(ctx, db.ListPendingBlobChannelsPageParams{
			Limit: reconcileBatch, Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("扫描未完成副本: %w", err)
		}
		for _, row := range rows {
			if _, err := r.client.Insert(ctx, ReplicateArgs{BlobID: row.BlobID, ChannelID: row.ChannelID}, nil); err != nil {
				return fmt.Errorf("重建任务(blob %d → 渠道 %d): %w", row.BlobID, row.ChannelID, err)
			}
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

// replicateWorker 单份副本的复制执行器。
type replicateWorker struct {
	river.WorkerDefaults[ReplicateArgs]
	// cfg Runner 依赖(共享)。
	cfg RunnerConfig
}

// Timeout 单任务执行超时(River 到时中断,防慢渠道占死 worker)。
func (w *replicateWorker) Timeout(*river.Job[ReplicateArgs]) time.Duration {
	return replicateJobTimeout
}

// Work 执行一次副本复制:从任一就绪渠道读出,写入目标渠道,状态写回业务表。
// @Description: 幂等——副本已 done 直接返回;目标渠道不存在则取消任务并标记 failed;
// 复制失败回退 pending 并交由 River 重试,最后一次尝试失败标记 failed。
func (w *replicateWorker) Work(ctx context.Context, job *river.Job[ReplicateArgs]) error {
	args := job.Args
	q := w.cfg.Queries

	current, err := q.GetBlobChannel(ctx, db.GetBlobChannelParams{BlobID: args.BlobID, ChannelID: args.ChannelID})
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
func (w *replicateWorker) copyBlob(ctx context.Context, blob db.Blob, target channel.Handle) error {
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
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// markState 写回副本状态,失败仅告警(业务表兜底,不阻断任务流转)。
// @param attemptsDelta 尝试计数增量(仅置 uploading 时为 1,保证计数=实际尝试次数)
func (w *replicateWorker) markState(ctx context.Context, args ReplicateArgs, state, lastError string, attemptsDelta int64) {
	if err := w.cfg.Queries.MarkBlobChannel(ctx, db.MarkBlobChannelParams{
		State: state, LastError: lastError, Attempts: attemptsDelta,
		BlobID: args.BlobID, ChannelID: args.ChannelID,
	}); err != nil {
		w.cfg.Logger.Error("写回副本状态失败", "blob", args.BlobID, "channel", args.ChannelID, "err", err)
	}
}
