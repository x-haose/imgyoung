package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"
)

// ObjectPurger 按 URL 清理 CDN 边缘缓存的执行口(由 cdn.CloudflarePurger 实现)。
type ObjectPurger interface {
	// Purge 清理给定绝对 URL 的边缘缓存,幂等。
	Purge(ctx context.Context, urls []string) error
}

// purgeJobTimeout 单个 purge 任务的执行超时(纯 API 调用,无需复制级预算)。
const purgeJobTimeout = 2 * time.Minute

// CDNPurgeArgs 清理一组 URL 边缘缓存的任务参数。
type CDNPurgeArgs struct {
	URLs []string `json:"urls"` // 待清理的绝对 URL 列表
}

// Kind 任务类型标识(River 要求)。
func (CDNPurgeArgs) Kind() string { return "cdn_purge" }

// InsertOpts 按参数去重:同一 URL 集不重复入队。
func (CDNPurgeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// EnqueuePurge 入队一组 URL 的 CDN 缓存清理任务(幂等,按参数去重)。
// @Description: purge 未启用(构造时无 Purger)或列表为空时为空操作——调用方无需感知开关。
// 入队失败仅由调用方告警,不阻塞删除事务;边缘缓存最终由 CDN 自身 TTL 兜底过期。
// @param urls 待清理的绝对 URL 列表
// @return 入队失败时的错误
func (r *Runner) EnqueuePurge(ctx context.Context, urls []string) error {
	if r.cfg.Purger == nil || len(urls) == 0 {
		return nil
	}
	if _, err := r.client.Insert(ctx, CDNPurgeArgs{URLs: urls}, nil); err != nil {
		return fmt.Errorf("入队 CDN purge 任务(%d 个 URL): %w", len(urls), err)
	}
	return nil
}

// cdnPurgeWorker CDN 缓存清理执行器(删除级联的异步收尾)。
type cdnPurgeWorker struct {
	river.WorkerDefaults[CDNPurgeArgs]
	cfg RunnerConfig // Runner 依赖(共享,取 Purger 与日志)
}

// Timeout 单任务执行超时。
func (*cdnPurgeWorker) Timeout(*river.Job[CDNPurgeArgs]) time.Duration {
	return purgeJobTimeout
}

// Work 清理一组 URL 的边缘缓存。
// @Description: Purger 未配置时直接成功——purge 配置关停后遗留任务安静消化,
// 与 EnqueuePurge 的空操作语义对齐;边缘缓存由 CDN 自身 TTL 兜底过期。
// purge 幂等,失败整体交 River 退避重试;最终失败也无数据风险——
// 源站对象已删,边缘缓存至多存活到 CDN 自身 TTL 到期。
func (w *cdnPurgeWorker) Work(ctx context.Context, job *river.Job[CDNPurgeArgs]) error {
	if w.cfg.Purger == nil {
		return nil
	}
	if err := w.cfg.Purger.Purge(ctx, job.Args.URLs); err != nil {
		return fmt.Errorf("清理 CDN 缓存(%d 个 URL): %w", len(job.Args.URLs), err)
	}
	w.cfg.Logger.Info("CDN 缓存已清理", "urls", len(job.Args.URLs))
	return nil
}
