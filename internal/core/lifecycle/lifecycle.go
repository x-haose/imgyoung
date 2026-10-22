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

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// sweepBatch 单轮扫描处理的上限;超出的留待下一轮,避免长事务与无限循环。
const sweepBatch = 500

// ErrImageNotFound 目标引用不存在。
var ErrImageNotFound = errors.New("lifecycle: 引用不存在")

// ErrImageExpired 引用已到期;到期是终态,有效期不再可改。
var ErrImageExpired = errors.New("lifecycle: 引用已到期")

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
	Conn     *sql.DB                // 数据库连接(多步删除的事务用)
	Queries  *sqlc.Queries          // sqlc 查询集
	Deleter  Deleter                // 渠道对象删除入队口
	Variants VariantCleaner         // 衍生图缓存清理;为 nil 时跳过
	Purger   PurgeEnqueuer          // CDN 缓存清理入队口;为 nil 时跳过
	Settings func() config.Settings // 运营配置快照读取口(逐轮/逐次取值,热生效);物理删宽限/冷图阈值/purge 启用与 base_url 出自它
	Logger   *slog.Logger           // 结构化日志
}

// Sweeper 清理器,无可变状态、并发安全。
type Sweeper struct {
	cfg Config // 依赖
}

// New 创建清理器。
// @param cfg 依赖,字段均必填(Deleter 除外可为 nil 时跳过级联入队)
// @return 就绪的清理器
func New(cfg Config) *Sweeper { return &Sweeper{cfg: cfg} }

// Run 周期清理,直至 ctx 取消;启动即先跑一轮。
// @param wait 周期闸,到点放行、ctx 取消即收敛
func (s *Sweeper) Run(ctx context.Context, wait func(context.Context) bool) {
	if err := s.SweepOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.cfg.Logger.Warn("启动清理未完成", "err", err)
	}
	for wait(ctx) {
		if err := s.SweepOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.cfg.Logger.Warn("定期清理未完成", "err", err)
		}
	}
}

// SweepOnce 一轮清理:回收到期引用 → 物理删已过宽限的物理图 → 冷图打标 → 回收超期幂等键。
// @Description: 每轮各处理至多 sweepBatch 条,积压留待下一轮追赶(不在单轮内长循环)。
// @return 扫描查询失败时的错误(单条删除失败只告警、不中断本轮)
func (s *Sweeper) SweepOnce(ctx context.Context) error {
	if err := s.expireImages(ctx); err != nil {
		return err
	}
	if err := s.physicalDelete(ctx); err != nil {
		return err
	}
	if err := s.labelColdImages(ctx); err != nil {
		return err
	}
	if err := s.expireIdempotencyKeys(ctx); err != nil {
		return err
	}
	return s.expireAuditLog(ctx)
}

// expireAuditLog 回收超过保留期的审计日志;保留期为零表示永久保留,不删。
// @Description: 保留天数逐轮取配置,热生效;只按记录时刻裁剪,不区分动作类型。
// @return 删除失败时的错误
func (s *Sweeper) expireAuditLog(ctx context.Context) error {
	retention := s.cfg.Settings().AuditRetention()
	if retention <= 0 {
		return nil
	}
	n, err := s.cfg.Queries.DeleteAuditLogBefore(ctx, sqlc.TimeText(time.Now().Add(-retention)))
	if err != nil {
		return fmt.Errorf("回收超期审计日志: %w", err)
	}
	if n > 0 {
		s.cfg.Logger.Info("超期审计日志回收", "count", n)
	}
	return nil
}

// expireIdempotencyKeys 回收超过保留期的上传幂等键。
// @Description: 幂等键的职责是吸收客户端的网络重试,保留期之外再命中只会把陈旧结果当新上传返回;
// 表也不能无限增长。保留期逐轮取配置,热生效。
// @return 删除失败时的错误
func (s *Sweeper) expireIdempotencyKeys(ctx context.Context) error {
	cutoff := sqlc.TimeText(time.Now().Add(-s.cfg.Settings().IdempotencyKeyTTL()))
	n, err := s.cfg.Queries.DeleteExpiredIdempotencyKeys(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("回收超期幂等键: %w", err)
	}
	if n > 0 {
		s.cfg.Logger.Info("超期幂等键回收", "count", n)
	}
	return nil
}

// labelColdImages 给超阈值未被公开访问的引用打 pending_cleanup 标签(不自动删,进后台待裁决)。
// @Description: 冷判据取 COALESCE(last_public_access_at, created_at);阈值 <=0 时跳过。
// 有真实访问会在活跃度 flush 时摘除标签(Tracker)。
// @return 更新失败时的错误
func (s *Sweeper) labelColdImages(ctx context.Context) error {
	threshold := time.Duration(s.cfg.Settings().ColdCleanupDays) * 24 * time.Hour
	if threshold <= 0 {
		return nil
	}
	cutoff := sql.NullString{String: sqlc.TimeText(time.Now().Add(-threshold)), Valid: true}
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
	// 不论是否已到期都可删:到期但尚未被扫描回收的引用同样在这里删掉。
	blobID, err := s.cfg.Queries.GetImageBlobID(ctx, imageID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrImageNotFound
	case err != nil:
		return fmt.Errorf("查询引用: %w", err)
	}
	return s.expireOne(ctx, imageID, blobID)
}

// ImagePatch 一次引用修改的内容;字段为 nil 表示不改该项,至少一项非 nil。
type ImagePatch struct {
	TTL           *time.Duration // 新的有效时长,从现在起算并记为该图的时长;0 表示改为永久
	CleanupExempt *bool          // 清理豁免;无论方向都摘掉待清理标签
	Private       *bool          // 私有图开关
}

// UpdateImage 在单事务内修改一条引用的有效期、清理豁免与私有开关,任一项失败整次回滚。
// @Description: 已到期的引用不再改有效期,判断与修改在同一条 UPDATE 里完成。豁免开关无论方向都摘
// pending_cleanup:置豁免即裁决保留,取消豁免交下轮冷图扫描重判。
// @param ctx 控制超时与取消
// @param imageID 目标引用短 ID
// @param patch 修改项
// @return ErrImageNotFound / ErrImageExpired 或基础设施错误
func (s *Sweeper) UpdateImage(ctx context.Context, imageID string, patch ImagePatch) error {
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := s.cfg.Queries.WithTx(tx)
	if patch.TTL != nil {
		if err := updateExpiry(ctx, q, imageID, *patch.TTL); err != nil {
			return err
		}
	}
	if err := setImageFlags(ctx, q, imageID, patch); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交引用修改: %w", err)
	}
	return nil
}

// setImageFlags 改一条引用的清理豁免与私有开关。
// @param q 事务内查询集
// @param imageID 目标引用短 ID
// @param patch 修改项,只处理其中的清理豁免与私有开关
// @return 引用不存在时的 ErrImageNotFound;落库失败时的错误
func setImageFlags(ctx context.Context, q *sqlc.Queries, imageID string, patch ImagePatch) error {
	if patch.CleanupExempt != nil {
		n, err := q.SetImageCleanupExempt(ctx, sqlc.SetImageCleanupExemptParams{
			Exempt: boolInt(*patch.CleanupExempt), ID: imageID,
		})
		if err != nil {
			return fmt.Errorf("更新清理豁免: %w", err)
		}
		if n == 0 {
			return ErrImageNotFound
		}
	}
	if patch.Private != nil {
		n, err := q.SetImagePrivate(ctx, sqlc.SetImagePrivateParams{Private: boolInt(*patch.Private), ID: imageID})
		if err != nil {
			return fmt.Errorf("更新私有开关: %w", err)
		}
		if n == 0 {
			return ErrImageNotFound
		}
	}
	return nil
}

// updateExpiry 改一条未到期引用的有效期。
// @param q 事务内查询集
// @param imageID 目标引用短 ID
// @param ttl 新的有效时长,从现在起算;0 表示永久
// @return 引用不存在时的 ErrImageNotFound;已到期时的 ErrImageExpired;落库失败时的错误
func updateExpiry(ctx context.Context, q *sqlc.Queries, imageID string, ttl time.Duration) error {
	now := time.Now()
	expireAt, ttlSeconds := sql.NullString{}, sql.NullInt64{}
	if ttl > 0 {
		expireAt = sql.NullString{String: sqlc.TimeText(now.Add(ttl)), Valid: true}
		ttlSeconds = sql.NullInt64{Int64: int64(ttl / time.Second), Valid: true}
	}
	n, err := q.UpdateImageExpiry(ctx, sqlc.UpdateImageExpiryParams{
		ExpireAt: expireAt, TtlSeconds: ttlSeconds, ID: imageID, Now: sqlc.TimeText(now),
	})
	if err != nil {
		return fmt.Errorf("更新有效期: %w", err)
	}
	if n > 0 {
		return nil
	}
	// 没有命中:引用不存在,或已到期。
	if _, err := q.GetImageOwner(ctx, imageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrImageNotFound
		}
		return fmt.Errorf("查询引用: %w", err)
	}
	return ErrImageExpired
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
	// 物理图取自被删掉的那一行,不用事务外读到的值:期间查重合并可能已把这条引用改指向别的物理图。
	liveBlobID, err := q.DeleteImage(ctx, imageID)
	if errors.Is(err, sql.ErrNoRows) {
		// 引用已被并发路径回收,计数已由该路径处理,本次不再触碰。
		return tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("删除引用: %w", err)
	}
	blobID = liveBlobID
	refCount, err := q.DecrementBlobRef(ctx, blobID)
	if err != nil {
		return fmt.Errorf("递减引用计数: %w", err)
	}
	if refCount <= 0 {
		grace := time.Duration(s.cfg.Settings().DeleteGraceMinutes) * time.Minute
		deleteAfter := sql.NullString{String: sqlc.TimeText(time.Now().Add(grace)), Valid: true}
		mark := sqlc.MarkBlobForDeleteParams{DeleteAfter: deleteAfter, ID: blobID}
		if err := q.MarkBlobForDelete(ctx, mark); err != nil {
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
// @Description: purge 未启用(无 Purger 或配置不齐)时返回 nil,不做无谓枚举;
// 枚举失败降级为只清原图 URL。format=auto 等其余查询串形态无法穷举,
// 漏网 URL 由 CDN 自身缓存 TTL 兜底过期。
// @param imageID 引用短 ID
// @param blobID 引用指向的物理图主键
// @return 待清理的绝对 URL 列表;purge 未启用时为 nil
func (s *Sweeper) collectPurgeURLs(ctx context.Context, imageID string, blobID int64) []string {
	set := s.cfg.Settings()
	if s.cfg.Purger == nil || !set.CDNPurgeEnabled() {
		return nil
	}
	base := set.BaseURL + "/i/" + imageID
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

// deleteBlob 物理删一个物理图:清衍生图缓存,取副本渠道清单,事务内删业务行,再级联入队删渠道对象。
// @Description: 渠道对象删除走 best-effort 入队——失败即对象成孤儿,由对账 GC 最终回收,
// 不阻塞逻辑删。CDN purge 不在此层:物理删时引用行已全部消亡,无公开 URL 可还原,
// 每条引用的 purge 已在 expireOne 回收该引用时逐条入队。
// @return 缓存清理、查询或事务失败时的错误
func (s *Sweeper) deleteBlob(ctx context.Context, blobID int64, storageKey string) error {
	// 先清衍生图缓存(文件+索引):variants 有外键指向 blobs,不先删其行,DeleteBlob 会被外键约束拦下。
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
	if err := s.deleteBlobRows(ctx, blobID); err != nil {
		return err
	}
	s.enqueueObjectDeletes(ctx, blobID, storageKey, channelIDs)
	return nil
}

// deleteBlobRows 在单事务内删除物理图及其副本记录、衍生图索引、相似候选。
// @Description: 事务内复查:扫描到物理删之间 blob 若被重新引用或撤销待删标记,删除条件不再成立、
// 影响 0 行,整个事务回滚——副本记录与变体索引随之保留,不会留下一个有引用却无副本的物理图。
// @return 事务失败或物理图已不满足删除条件时的错误
func (s *Sweeper) deleteBlobRows(ctx context.Context, blobID int64) error {
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.cfg.Queries.WithTx(tx)
	if err := q.DeleteBlobChannelsByBlob(ctx, blobID); err != nil {
		return fmt.Errorf("删除副本记录: %w", err)
	}
	// 无条件删 variant 索引行作外键兜底:缓存清理已删则此处 0 行;Variants 为 nil 而有残留行时
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
	deleted, err := q.DeleteBlob(ctx, blobID)
	if err != nil {
		return fmt.Errorf("删除物理图: %w", err)
	}
	if deleted == 0 {
		return errors.New("物理图已被重新引用或撤销待删,放弃本次物理删")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交物理删: %w", err)
	}
	return nil
}

// enqueueObjectDeletes 为物理图在各渠道的对象入队删除任务;入队口未配置时跳过,单个入队失败只告警。
// @param blobID 物理图主键(日志定位用)
// @param storageKey 对象键
// @param channelIDs 持有该对象的渠道
func (s *Sweeper) enqueueObjectDeletes(ctx context.Context, blobID int64, storageKey string,
	channelIDs []int64) {
	if s.cfg.Deleter == nil {
		return
	}
	for _, chID := range channelIDs {
		if err := s.cfg.Deleter.EnqueueDelete(ctx, chID, storageKey); err != nil {
			s.cfg.Logger.Warn("入队删除渠道对象失败,待对账 GC 兜底", "blob", blobID, "channel", chID, "err", err)
		}
	}
}

// boolInt 布尔转 SQLite 的 0/1 整数。
// @param b 布尔值
// @return 1 表示 true,0 表示 false
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
