package replication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db"
)

// gcInterval 对账 GC 周期(设计定稿·对账 GC:每日低频任务)。
const gcInterval = 24 * time.Hour

// orphanGrace 孤儿对象宽限期:修改时间在此窗口内的未知对象不回收,
// 覆盖"渠道已写入、落库尚未完成"的正常中间态。
const orphanGrace = 48 * time.Hour

// systemPrefix 渠道内系统对象目录前缀,对账一律豁免;
// reliability.Dir 等系统目录均约定置于该前缀之下。
const systemPrefix = "_imgyoung/"

// GCResult 单渠道一轮对账的结果统计。
type GCResult struct {
	// Channel 渠道名。
	Channel string
	// Objects 渠道对象总数(不含系统对象)。
	Objects int
	// Bytes 渠道对象总字节(不含系统对象)。
	Bytes int64
	// OrphansDeleted 回收的孤儿对象数。
	OrphansDeleted int
	// PhantomsRequeued 重置补齐的幻影副本数。
	PhantomsRequeued int
}

// RunGC 对全部健康渠道执行一轮双向对账(设计定稿·对账 GC)。
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

// gcChannel 对单个渠道做双向对账。
// @param h 目标渠道
// @return 对账统计
// @return 列举或查询失败时的错误
func (r *Runner) gcChannel(ctx context.Context, h channel.Handle) (GCResult, error) {
	res := GCResult{Channel: h.Name}

	objs, err := h.Driver.List(ctx, "")
	if err != nil {
		return res, fmt.Errorf("列举渠道对象: %w", err)
	}
	onChannel := make(map[string]int64, len(objs))
	var orphanCandidates []string
	for _, o := range objs {
		if strings.HasPrefix(o.Key, systemPrefix) || o.Key == channel.HealthProbeKey {
			continue
		}
		res.Objects++
		res.Bytes += o.Size
		onChannel[o.Key] = o.Size
		if time.Since(o.ModTime) > orphanGrace {
			orphanCandidates = append(orphanCandidates, o.Key)
		}
	}

	rows, err := r.cfg.Queries.ListDoneBlobChannelsForChannel(ctx, h.ID)
	if err != nil {
		return res, fmt.Errorf("查询渠道就绪副本: %w", err)
	}
	inDB := make(map[string]int64, len(rows))
	for _, row := range rows {
		inDB[row.StorageKey] = row.BlobID
	}

	// 孤儿:渠道有、DB 无、已过宽限期 → 回收。
	for _, key := range orphanCandidates {
		if _, ok := inDB[key]; ok {
			continue
		}
		if derr := h.Driver.Delete(ctx, key); derr != nil {
			r.cfg.Logger.Warn("孤儿对象回收失败", "channel", h.Name, "key", key, "err", derr)
			continue
		}
		res.OrphansDeleted++
	}

	// 幻影:DB 记为就绪、渠道缺失 → 回退 pending 并重新入队补齐。
	for key, blobID := range inDB {
		if _, ok := onChannel[key]; ok {
			continue
		}
		if merr := r.cfg.Queries.MarkBlobChannel(ctx, db.MarkBlobChannelParams{
			State: channel.StatePending, LastError: "对账:渠道对象缺失", Attempts: 0,
			BlobID: blobID, ChannelID: h.ID,
		}); merr != nil {
			r.cfg.Logger.Warn("幻影副本重置失败", "channel", h.Name, "key", key, "err", merr)
			continue
		}
		if _, ierr := r.client.Insert(ctx, ReplicateArgs{BlobID: blobID, ChannelID: h.ID}, nil); ierr != nil {
			r.cfg.Logger.Warn("幻影副本补齐入队失败,待恢复扫描兜底", "channel", h.Name, "key", key, "err", ierr)
		}
		res.PhantomsRequeued++
	}

	return res, nil
}
