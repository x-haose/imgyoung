// Package lifecycle 生命周期与清理(设计定稿·生命周期与清理):
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

	"github.com/x-haose/imgyoung/internal/infra/db"
)

// sweepBatch 单轮扫描处理的上限;超出的留待下一轮,避免长事务与无限循环。
const sweepBatch = 500

// Deleter 渠道对象删除入队口(由 replication.Runner 实现),解耦 lifecycle 与 River。
type Deleter interface {
	// EnqueueDelete 入队删除某渠道内一个对象。
	EnqueueDelete(ctx context.Context, channelID int64, storageKey string) error
}

// Config Sweeper 的依赖与参数。
type Config struct {
	// Conn 数据库连接(多步删除的事务用)。
	Conn *sql.DB
	// Queries sqlc 查询集。
	Queries *db.Queries
	// Deleter 渠道对象删除入队口。
	Deleter Deleter
	// Grace 物理删宽限:引用归零打标到实际物理删的等待,可为 0(设计定稿·删除三段式)。
	Grace time.Duration
	// ColdThreshold 冷图待清理阈值:超过此时长未被公开访问的引用打 pending_cleanup 标签;
	// <=0 表示不启用(设计定稿·待清理标签:机器发现,人做不可逆决定)。
	ColdThreshold time.Duration
	// Logger 结构化日志。
	Logger *slog.Logger
}

// Sweeper 清理器,无可变状态、并发安全。
type Sweeper struct {
	// cfg 依赖。
	cfg Config
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
	cutoff := sql.NullString{String: db.TimeText(time.Now().Add(-s.cfg.ColdThreshold)), Valid: true}
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
	now := sql.NullString{String: db.TimeText(time.Now()), Valid: true}
	rows, err := s.cfg.Queries.ListExpiredImages(ctx, db.ListExpiredImagesParams{ExpireAt: now, Limit: sweepBatch})
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

// expireOne 在单事务内回收一条到期引用(引用计数是唯一真相,故删引用与减计数同事务)。
// @return 事务失败时的错误
func (s *Sweeper) expireOne(ctx context.Context, imageID string, blobID int64) error {
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := s.cfg.Queries.WithTx(tx)
	if err := q.DeleteIdempotencyKeysByImage(ctx, imageID); err != nil {
		return fmt.Errorf("清理幂等键: %w", err)
	}
	if err := q.DeleteImage(ctx, imageID); err != nil {
		return fmt.Errorf("删除引用: %w", err)
	}
	refCount, err := q.DecrementBlobRef(ctx, blobID)
	if err != nil {
		return fmt.Errorf("递减引用计数: %w", err)
	}
	if refCount <= 0 {
		deleteAfter := sql.NullString{String: db.TimeText(time.Now().Add(s.cfg.Grace)), Valid: true}
		if err := q.MarkBlobForDelete(ctx, db.MarkBlobForDeleteParams{DeleteAfter: deleteAfter, ID: blobID}); err != nil {
			return fmt.Errorf("标记物理图待删: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交到期回收: %w", err)
	}
	return nil
}

// physicalDelete 物理删已过宽限的物理图:事务内删业务行,再级联入队删渠道对象。
// @return 扫描失败时的错误
func (s *Sweeper) physicalDelete(ctx context.Context) error {
	now := sql.NullString{String: db.TimeText(time.Now()), Valid: true}
	rows, err := s.cfg.Queries.ListBlobsToDelete(ctx, db.ListBlobsToDeleteParams{DeleteAfter: now, Limit: sweepBatch})
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
// @Description: 渠道对象删除走 best-effort 入队——失败即对象成孤儿,由对账 GC 最终回收
// (设计定稿·删除重试队列由对账兜底),不阻塞逻辑删。
// @return 事务失败时的错误
func (s *Sweeper) deleteBlob(ctx context.Context, blobID int64, storageKey string) error {
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
