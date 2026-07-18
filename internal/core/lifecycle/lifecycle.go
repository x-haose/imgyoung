// Package lifecycle 生命周期与清理:
// TTL 到期扫描 → 引用计数递减 → 三段式物理删(标记 → 宽限 → 物理删)级联渠道副本。
// 基于查询兜底——进程宕机漏扫,重启后按 expire_at / delete_after 自动追赶。
package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// sweepBatch 单轮扫描处理的上限;超出的留待下一轮,避免长事务与无限循环。
const sweepBatch = 500

// ErrImageNotFound 目标引用不存在。
var ErrImageNotFound = errors.New("lifecycle: 引用不存在")

// Deleter 渠道对象删除入队口(由 replication.Runner 实现),解耦 lifecycle 与 River。
type Deleter interface {
	// EnqueueDelete 入队删除某渠道内一个对象。
	EnqueueDelete(ctx context.Context, channelID int64, storageKey string) error
}

// VariantCleaner 删除某物理图的全部衍生图缓存(文件+索引,由 variants.Store 实现)。
type VariantCleaner interface {
	// DeleteByBlob 删除某物理图的全部衍生图缓存。
	DeleteByBlob(ctx context.Context, blobID int64) error
}

// PurgeEnqueuer CDN 边缘缓存清理入队口(由 replication.Runner 实现):
// 引用删除级联时异步清理其公开 URL 的边缘缓存,失败由任务队列退避重试。
type PurgeEnqueuer interface {
	// EnqueuePurge 入队一组绝对 URL 的边缘缓存清理任务(幂等)。
	EnqueuePurge(ctx context.Context, urls []string) error
}

// Config Sweeper 的依赖与参数。
type Config struct {
	Conn          *sql.DB        // 数据库连接(多步删除的事务用)
	Queries       *sqlc.Queries  // sqlc 查询集
	Deleter       Deleter        // 渠道对象删除入队口
	Variants      VariantCleaner // 衍生图缓存清理;为 nil 时跳过
	Purger        PurgeEnqueuer  // CDN 缓存清理入队口;为 nil 时跳过
	BaseURL       string         // 对外站点地址(无尾斜杠),拼 purge 绝对 URL;purge 只认配置值——异步任务无请求上下文可推导,为空时跳过 purge
	Grace         time.Duration  // 物理删宽限:引用归零打标到实际物理删的等待,可为 0
	ColdThreshold time.Duration  // 冷图待清理阈值:超过此时长未被公开访问的引用打 pending_cleanup 标签;<=0 表示不启用
	Logger        *slog.Logger   // 结构化日志
}

// Sweeper 清理器,无可变状态、并发安全。
type Sweeper struct {
	cfg Config // 依赖
}

// New 创建清理器。
// @param cfg 依赖,字段均必填(Deleter 除外可为 nil 时跳过级联入队)
// @return 就绪的清理器
func New(cfg Config) *Sweeper { return &Sweeper{cfg: cfg} }

// Run 按 interval 周期清理,直至 ctx 取消;启动即先跑一轮。
// @param interval 扫描间隔
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if err := s.SweepOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.cfg.Logger.Warn("启动清理未完成", "err", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SweepOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.cfg.Logger.Warn("定期清理未完成", "err", err)
			}
		}
	}
}

// SweepOnce 一轮清理:先回收到期引用,再物理删已过宽限的物理图。
// @Description: 每轮各处理至多 sweepBatch 条,积压留待下一轮追赶(不在单轮内长循环)。
// @return 扫描查询失败时的错误(单条删除失败只告警、不中断本轮)
func (s *Sweeper) SweepOnce(ctx context.Context) error {
	if err := s.expireImages(ctx); err != nil {
		return err
	}
	if err := s.physicalDelete(ctx); err != nil {
		return err
	}
	return s.labelColdImages(ctx)
}

// labelColdImages 给超阈值未被公开访问的引用打 pending_cleanup 标签(不自动删,进后台待裁决)。
// @Description: 冷判据取 COALESCE(last_public_access_at, created_at);ColdThreshold<=0 时跳过。
// 有真实访问会在活跃度 flush 时摘除标签(Tracker)。
// @return 更新失败时的错误
func (s *Sweeper) labelColdImages(ctx context.Context) error {
	if s.cfg.ColdThreshold <= 0 {
		return nil
	}
	cutoff := sql.NullString{String: sqlc.TimeText(time.Now().Add(-s.cfg.ColdThreshold)), Valid: true}
	n, err := s.cfg.Queries.LabelColdImages(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("标记冷图待清理: %w", err)
	}
	if n > 0 {
		s.cfg.Logger.Info("冷图打待清理标签", "count", n)
	}
	return nil
}

// expireImages 回收到期引用:删引用与其幂等键、递减物理图引用计数,归零则打物理删标记。
// @return 扫描失败时的错误
func (s *Sweeper) expireImages(ctx context.Context) error {
	now := sql.NullString{String: sqlc.TimeText(time.Now()), Valid: true}
	rows, err := s.cfg.Queries.ListExpiredImages(ctx, sqlc.ListExpiredImagesParams{ExpireAt: now, Limit: sweepBatch})
	if err != nil {
		return fmt.Errorf("扫描到期引用: %w", err)
	}
	for _, row := range rows {
		if err := s.expireOne(ctx, row.ID, row.BlobID); err != nil {
			s.cfg.Logger.Warn("回收到期引用失败", "image", row.ID, "err", err)
		}
	}
	if len(rows) > 0 {
		s.cfg.Logger.Info("到期引用回收", "count", len(rows))
	}
	return nil
}

// DeleteImage 立即删除一条引用(后台管理删除入口)。
// @Description: 事务语义与到期回收完全一致:删引用与幂等键、递减引用计数,
// 归零则按宽限期打物理删标记,级联由 Sweeper 后续轮次完成。
// @param ctx 控制超时与取消
// @param imageID 目标引用短 ID
// @return ErrImageNotFound 或基础设施错误
func (s *Sweeper) DeleteImage(ctx context.Context, imageID string) error {
	row, err := s.cfg.Queries.GetImageBlob(ctx, imageID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrImageNotFound
	case err != nil:
		return fmt.Errorf("查询引用: %w", err)
	}
	return s.expireOne(ctx, row.ID, row.BlobID)
}

// expireOne 在单事务内回收一条到期引用(引用计数是唯一真相,故删引用与减计数同事务)。
// @return 事务失败时的错误
func (s *Sweeper) expireOne(ctx context.Context, imageID string, blobID int64) error {
	// 先于删除收集 purge URL:变体索引行到物理删阶段才消亡,此刻仍可枚举齐全。
	purgeURLs := s.collectPurgeURLs(ctx, imageID, blobID)
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := s.cfg.Queries.WithTx(tx)
	if err := q.DeleteIdempotencyKeysByImage(ctx, imageID); err != nil {
		return fmt.Errorf("清理幂等键: %w", err)
	}
	// 删引用与减计数必须原子:仅当本事务确实删掉了引用行才减 ref_count,
	// 否则重复回收同一引用(管理端删除与到期扫描并发命中同图)会多减、
	// 把仍被其他引用共享的 blob 误减到 0 而误删。
	deleted, err := q.DeleteImage(ctx, imageID)
	if err != nil {
		return fmt.Errorf("删除引用: %w", err)
	}
	if deleted == 0 {
		// 引用已被并发路径回收,计数已由该路径处理,本次不再触碰。
		return tx.Commit()
	}
	refCount, err := q.DecrementBlobRef(ctx, blobID)
	if err != nil {
		return fmt.Errorf("递减引用计数: %w", err)
	}
	if refCount <= 0 {
		deleteAfter := sql.NullString{String: sqlc.TimeText(time.Now().Add(s.cfg.Grace)), Valid: true}
		if err := q.MarkBlobForDelete(ctx, sqlc.MarkBlobForDeleteParams{DeleteAfter: deleteAfter, ID: blobID}); err != nil {
			return fmt.Errorf("标记物理图待删: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交到期回收: %w", err)
	}
	// 引用已死,其公开 URL 随之失效:异步清 CDN 边缘缓存,不阻塞删除。
	s.enqueuePurge(ctx, imageID, purgeURLs)
	return nil
}

// collectPurgeURLs 枚举一条引用对外可能被 CDN 缓存的 URL:原图 /i/{id},
// 加上该物理图实际生成过的变体参数组合(显式 ?w=&format= 与协商 ?w= 两种形态)。
// @Description: purge 未启用(无 Purger 或无 BaseURL)时返回 nil,不做无谓枚举;
// 枚举失败降级为只清原图 URL。format=auto 等其余查询串形态无法穷举,
// 漏网 URL 由 CDN 自身缓存 TTL 兜底过期。
// @param imageID 引用短 ID
// @param blobID 引用指向的物理图主键
// @return 待清理的绝对 URL 列表;purge 未启用时为 nil
func (s *Sweeper) collectPurgeURLs(ctx context.Context, imageID string, blobID int64) []string {
	if s.cfg.Purger == nil || s.cfg.BaseURL == "" {
		return nil
	}
	base := s.cfg.BaseURL + "/i/" + imageID
	urls := []string{base}
	rows, err := s.cfg.Queries.ListVariantURLParams(ctx, blobID)
	if err != nil {
		s.cfg.Logger.Warn("枚举变体参数失败,仅清理原图 URL", "image", imageID, "err", err)
		return urls
	}
	seen := map[string]struct{}{base: {}}
	add := func(u string) {
		if _, ok := seen[u]; !ok {
			seen[u] = struct{}{}
			urls = append(urls, u)
		}
	}
	for _, v := range rows {
		if v.Width > 0 {
			add(fmt.Sprintf("%s?w=%d", base, v.Width))
			add(fmt.Sprintf("%s?w=%d&format=%s", base, v.Width, v.Format))
		} else {
			add(base + "?format=" + v.Format)
		}
	}
	return urls
}

// enqueuePurge 入队 CDN 缓存清理;失败仅告警,绝不阻塞删除流程。
// @param imageID 引用短 ID(日志定位用)
// @param urls 待清理 URL;空列表为空操作
func (s *Sweeper) enqueuePurge(ctx context.Context, imageID string, urls []string) {
	if len(urls) == 0 {
		return
	}
	if err := s.cfg.Purger.EnqueuePurge(ctx, urls); err != nil {
		s.cfg.Logger.Warn("CDN purge 入队失败,边缘缓存待 CDN TTL 自然过期", "image", imageID, "err", err)
	}
}

// physicalDelete 物理删已过宽限的物理图:事务内删业务行,再级联入队删渠道对象。
// @return 扫描失败时的错误
func (s *Sweeper) physicalDelete(ctx context.Context) error {
	now := sql.NullString{String: sqlc.TimeText(time.Now()), Valid: true}
	rows, err := s.cfg.Queries.ListBlobsToDelete(ctx, sqlc.ListBlobsToDeleteParams{DeleteAfter: now, Limit: sweepBatch})
	if err != nil {
		return fmt.Errorf("扫描待物理删物理图: %w", err)
	}
	for _, row := range rows {
		if err := s.deleteBlob(ctx, row.ID, row.StorageKey); err != nil {
			s.cfg.Logger.Warn("物理删物理图失败", "blob", row.ID, "err", err)
		}
	}
	if len(rows) > 0 {
		s.cfg.Logger.Info("物理图物理删", "count", len(rows))
	}
	return nil
}

// deleteBlob 物理删一个物理图:先取副本渠道清单,事务内删业务行,再级联入队删渠道对象。
// @Description: 渠道对象删除走 best-effort 入队——失败即对象成孤儿,由对账 GC 最终回收,
// 不阻塞逻辑删。CDN purge 不在此层:物理删时引用行已全部消亡,无公开 URL 可还原,
// 每条引用的 purge 已在 expireOne 回收该引用时逐条入队。
// @return 事务失败时的错误
func (s *Sweeper) deleteBlob(ctx context.Context, blobID int64, storageKey string) error {
	// 先清衍生图缓存(文件+索引):variants 有外键指向 blobs,不先删其行,下面的 DeleteBlob 会被外键约束拦下。
	// 缓存是可重建的派生物,即便后续事务回滚也只是让其冷启,无数据风险。
	if s.cfg.Variants != nil {
		if err := s.cfg.Variants.DeleteByBlob(ctx, blobID); err != nil {
			return fmt.Errorf("清理衍生图缓存: %w", err)
		}
	}

	// 删业务行前取副本渠道清单(删后就查不到了)。
	channelIDs, err := s.cfg.Queries.ListBlobChannelIDs(ctx, blobID)
	if err != nil {
		return fmt.Errorf("查询副本渠道: %w", err)
	}

	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.cfg.Queries.WithTx(tx)
	if err := q.DeleteBlobChannelsByBlob(ctx, blobID); err != nil {
		return fmt.Errorf("删除副本记录: %w", err)
	}
	// 无条件删 variant 索引行作外键兜底:上面 DeleteByBlob 已删则此处 0 行;若 Variants 为 nil 而有残留行,
	// 这里兜住,避免 DeleteBlob 撞外键约束致该物理图每轮重试删不掉(文件清理仍依赖 DeleteByBlob)。
	if err := q.DeleteVariantsByBlob(ctx, blobID); err != nil {
		return fmt.Errorf("删除衍生图索引: %w", err)
	}
	// 相似候选随物理图消亡(候选是工作队列不是历史,留痕在 audit_log)。
	if err := q.DeleteDedupCandidatesByBlob(ctx, sqlc.DeleteDedupCandidatesByBlobParams{
		BlobA: blobID, BlobB: blobID,
	}); err != nil {
		return fmt.Errorf("删除相似候选: %w", err)
	}
	if err := q.DeleteBlob(ctx, blobID); err != nil {
		return fmt.Errorf("删除物理图: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交物理删: %w", err)
	}

	for _, chID := range channelIDs {
		if s.cfg.Deleter == nil {
			continue
		}
		if err := s.cfg.Deleter.EnqueueDelete(ctx, chID, storageKey); err != nil {
			s.cfg.Logger.Warn("入队删除渠道对象失败,待对账 GC 兜底", "blob", blobID, "channel", chID, "err", err)
		}
	}
	return nil
}
