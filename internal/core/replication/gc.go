package replication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/store/failover"
)

// gcInterval 对账 GC 周期(每日低频任务)。
const gcInterval = 24 * time.Hour

// orphanGrace 孤儿对象宽限期:修改时间在此窗口内的未知对象不回收,
// 覆盖"渠道已写入、落库尚未完成"的正常中间态。
const orphanGrace = 48 * time.Hour

// systemPrefix 渠道内系统对象目录前缀,对账一律豁免;
// reliability.Dir 等系统目录均约定置于该前缀之下。
const systemPrefix = "_imgyoung/"

// GCResult 单渠道一轮对账的结果统计。
type GCResult struct {
	Channel          string // 渠道名
	Objects          int    // 渠道对象总数(不含系统对象)
	Bytes            int64  // 渠道对象总字节(不含系统对象)
	OrphansDeleted   int    // 回收的孤儿对象数
	PhantomsRequeued int    // 重置补齐的幻影副本数
}

// RunGC 对全部健康渠道执行一轮双向对账。
// @Description: 孤儿 = 渠道有、DB 无(超宽限期即回收,按量付费渠道防烧钱);
// 幻影 = DB 记为就绪、渠道实际缺失(回退 pending 并重新入队补齐)。
// 顺带产出各渠道用量统计。单渠道失败跳过,不中断其余渠道。
// @return 各渠道对账结果
// @return 全部渠道均失败时以外的错误不返回(逐渠道告警)
func (r *Runner) RunGC(ctx context.Context) ([]GCResult, error) {
	results := make([]GCResult, 0)
	for _, h := range r.cfg.Manager.Healthy() {
		res, err := r.gcChannel(ctx, h)
		if err != nil {
			r.cfg.Logger.Warn("渠道对账未完成", "channel", h.Name, "err", err)
			continue
		}
		results = append(results, res)
		r.cfg.Logger.Info("渠道对账完成",
			"channel", res.Channel, "objects", res.Objects, "bytes", res.Bytes,
			"orphans_deleted", res.OrphansDeleted, "phantoms_requeued", res.PhantomsRequeued)
	}
	return results, nil
}

// inventory 渠道上业务对象的盘点结果,系统目录与健康探测对象已排除。
type inventory struct {
	sizes map[string]int64 // 对象键 → 字节数
	stale []string         // 修改时间早于孤儿宽限期的对象键,孤儿回收只在其中选
	bytes int64            // 对象总字节
}

// gcChannel 对单个渠道做双向对账。
// @param h 目标渠道
// @return 对账统计
// @return 列举或查询失败时的错误
func (r *Runner) gcChannel(ctx context.Context, h channel.Handle) (GCResult, error) {
	res := GCResult{Channel: h.Name}
	inv, err := takeInventory(ctx, h)
	if err != nil {
		return res, err
	}
	res.Objects = len(inv.sizes)
	res.Bytes = inv.bytes

	rows, err := r.cfg.Queries.ListDoneBlobChannelsForChannel(ctx, h.ID)
	if err != nil {
		return res, fmt.Errorf("查询渠道就绪副本: %w", err)
	}
	inDB := make(map[string]int64, len(rows))
	for _, row := range rows {
		inDB[row.StorageKey] = row.BlobID
	}
	res.OrphansDeleted = r.reclaimOrphans(ctx, h, inv.stale, inDB)
	res.PhantomsRequeued = r.requeuePhantoms(ctx, h, inDB, inv.sizes)
	return res, nil
}

// takeInventory 列举渠道对象并盘点:统计总量,挑出已过宽限期的孤儿候选。
// @param h 目标渠道
// @return 盘点结果
// @return 列举失败时的错误
func takeInventory(ctx context.Context, h channel.Handle) (inventory, error) {
	objs, err := h.Driver.List(ctx, "")
	if err != nil {
		return inventory{}, fmt.Errorf("列举渠道对象: %w", err)
	}
	inv := inventory{sizes: make(map[string]int64, len(objs))}
	for _, o := range objs {
		if strings.HasPrefix(o.Key, systemPrefix) || o.Key == failover.ProbeKey {
			continue
		}
		inv.sizes[o.Key] = o.Size
		inv.bytes += o.Size
		if time.Since(o.ModTime) > orphanGrace {
			inv.stale = append(inv.stale, o.Key)
		}
	}
	return inv, nil
}

// reclaimOrphans 回收孤儿对象:渠道有、DB 无、已过宽限期。单个删除失败只告警,留待下一轮。
// @param h 目标渠道
// @param stale 已过宽限期的对象键
// @param inDB DB 记为就绪的对象键 → blob 主键
// @return 回收的对象数
func (r *Runner) reclaimOrphans(ctx context.Context, h channel.Handle, stale []string,
	inDB map[string]int64) int {
	deleted := 0
	for _, key := range stale {
		if _, ok := inDB[key]; ok {
			continue
		}
		if err := h.Driver.Delete(ctx, key); err != nil {
			r.cfg.Logger.Warn("孤儿对象回收失败", "channel", h.Name, "key", key, "err", err)
			continue
		}
		deleted++
	}
	return deleted
}

// requeuePhantoms 处理幻影副本:DB 记为就绪、渠道缺失的,回退 pending 并重新入队补齐。
// @Description: 复位失败的行跳过不计数;入队失败只告警,pending 行由下一轮对账扫描兜底入队。
// @param h 目标渠道
// @param inDB DB 记为就绪的对象键 → blob 主键
// @param onChannel 渠道上实际存在的对象键 → 字节数
// @return 复位并重新入队的副本数
func (r *Runner) requeuePhantoms(ctx context.Context, h channel.Handle,
	inDB, onChannel map[string]int64) int {
	requeued := 0
	for key, blobID := range inDB {
		if _, ok := onChannel[key]; ok {
			continue
		}
		if err := r.cfg.Queries.MarkBlobChannel(ctx, sqlc.MarkBlobChannelParams{
			State: channel.StatePending, LastError: "对账:渠道对象缺失", Attempts: 0,
			BlobID: blobID, ChannelID: h.ID,
		}); err != nil {
			r.cfg.Logger.Warn("幻影副本重置失败", "channel", h.Name, "key", key, "err", err)
			continue
		}
		if _, err := r.client.Insert(ctx, ReplicateArgs{BlobID: blobID, ChannelID: h.ID}, nil); err != nil {
			r.cfg.Logger.Warn("幻影副本补齐入队失败,待恢复扫描兜底", "channel", h.Name, "key", key, "err", err)
		}
		requeued++
	}
	return requeued
}
