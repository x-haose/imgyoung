package replication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/store/driver"
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
	Foreign          int    // 库里没有记录、不是本库写出的对象数(键名不是本服务格式,或修改时间早于渠道的绑定时刻);不回收
	OrphansDeleted   int    // 回收的孤儿对象数
	PhantomsRequeued int    // 重置补齐的幻影副本数
}

// RunGC 对全部健康渠道执行一轮双向对账。
// @Description: 孤儿 = 渠道有、库里没有任何状态的副本记录、键名是本服务的对象键格式、修改时间不早于
// 渠道的绑定时刻且已过宽限期,回收(按量付费渠道防烧钱)。本库只以该格式命名对象,且只在绑定时刻之后
// 往渠道当前位置写入;其余对象(别的程序写的文件、灾备接入的旧桶、恢复的快照之后写入的对象)只计数不回收。
// 深探测删除失败留下的临时对象过宽限期即回收。
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
			"channel", res.Channel, "objects", res.Objects, "bytes", res.Bytes, "foreign", res.Foreign,
			"orphans_deleted", res.OrphansDeleted, "phantoms_requeued", res.PhantomsRequeued)
	}
	return results, nil
}

// inventory 渠道上业务对象与库内副本记录的对照结果,系统目录与健康探测对象已排除。
type inventory struct {
	sizes   map[string]int64 // 对象键 → 字节数
	bytes   int64            // 对象总字节
	orphans []string         // 可回收的孤儿对象键
	foreign int              // 库里没有记录、不是本库写出的对象数
}

// gcChannel 对单个渠道做双向对账。
// @param h 目标渠道
// @return 对账统计
// @return 读取渠道记录、列举对象或查询副本记录失败时的错误
func (r *Runner) gcChannel(ctx context.Context, h channel.Handle) (GCResult, error) {
	res := GCResult{Channel: h.Name}
	row, err := r.cfg.Queries.GetChannel(ctx, h.ID)
	if err != nil {
		return res, fmt.Errorf("读取渠道记录: %w", err)
	}
	boundAt, err := sqlc.ParseTimeText(row.BoundAt)
	if err != nil {
		return res, err
	}
	objs, err := h.Driver.List(ctx, "")
	if err != nil {
		return res, fmt.Errorf("列举渠道对象: %w", err)
	}
	replicas, err := r.cfg.Queries.ListBlobChannelKeysForChannel(ctx, h.ID)
	if err != nil {
		return res, fmt.Errorf("查询渠道副本记录: %w", err)
	}
	known := make(map[string]struct{}, len(replicas))
	done := make(map[string]int64, len(replicas))
	for _, rep := range replicas {
		known[rep.StorageKey] = struct{}{}
		if rep.State == channel.StateDone {
			done[rep.StorageKey] = rep.BlobID
		}
	}
	inv := takeInventory(objs, known, boundAt, time.Now())
	res.Objects, res.Bytes, res.Foreign = len(inv.sizes), inv.bytes, inv.foreign
	res.OrphansDeleted = r.reclaimOrphans(ctx, h, inv.orphans)
	res.PhantomsRequeued = r.requeuePhantoms(ctx, h, done, inv.sizes)
	return res, nil
}

// takeInventory 盘点渠道对象:统计总量,对照库内副本记录挑出可回收的孤儿。
// @param objs 渠道上的全部对象
// @param known 库内该渠道任一状态副本记录的对象键
// @param boundAt 渠道的绑定时刻;本库只可能写出修改时间不早于它的对象
// @param now 盘点时刻,宽限期据此计算
// @return 盘点结果
func takeInventory(objs []driver.ObjectInfo, known map[string]struct{}, boundAt, now time.Time) inventory {
	inv := inventory{sizes: make(map[string]int64, len(objs))}
	for _, o := range objs {
		if strings.HasPrefix(o.Key, systemPrefix) || o.Key == failover.ProbeKey {
			continue
		}
		// 深探测残留:探后即删的临时对象,删除失败才会留下;不论哪个库写的都可回收,也不计入用量。
		if failover.IsDeepProbeKey(o.Key) {
			if now.Sub(o.ModTime) > orphanGrace {
				inv.orphans = append(inv.orphans, o.Key)
			}
			continue
		}
		inv.sizes[o.Key] = o.Size
		inv.bytes += o.Size
		if _, ok := known[o.Key]; ok {
			continue
		}
		switch {
		case !id.IsStorageKey(o.Key) || o.ModTime.Before(boundAt):
			inv.foreign++
		case now.Sub(o.ModTime) > orphanGrace:
			inv.orphans = append(inv.orphans, o.Key)
		default:
			// 宽限期内:可能是已写入渠道、尚未落库的在途上传,本轮不动。
		}
	}
	return inv
}

// reclaimOrphans 删除孤儿对象;单个删除失败只告警,留待下一轮。
// @param h 目标渠道
// @param orphans 孤儿对象键
// @return 回收的对象数
func (r *Runner) reclaimOrphans(ctx context.Context, h channel.Handle, orphans []string) int {
	deleted := 0
	for _, key := range orphans {
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
// @param done DB 记为就绪的对象键 → blob 主键
// @param onChannel 渠道上实际存在的对象键 → 字节数
// @return 复位并重新入队的副本数
func (r *Runner) requeuePhantoms(ctx context.Context, h channel.Handle,
	done, onChannel map[string]int64) int {
	requeued := 0
	for key, blobID := range done {
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
