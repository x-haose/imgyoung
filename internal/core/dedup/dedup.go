// Package dedup 后台查重与合并:机器发现,人做不可逆决定。
// 精确重复(SHA256 相同、字节完全一致)零风险自动合并;相似重复(pHash 汉明距离)
// 走人工裁决。查重是纯 DB 操作,不在上传热路径。
package dedup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// Config Merger 依赖与参数。
type Config struct {
	Conn    *sql.DB       // 数据库连接(合并事务用)
	Queries *sqlc.Queries // sqlc 查询集
	Grace   time.Duration // 冗余 blob 物理删宽限(与生命周期一致);合并后冗余 blob 交 sweeper 物理删
	Logger  *slog.Logger  // 结构化日志
}

// Merger 精确重复合并器,无可变状态、并发安全。
type Merger struct {
	cfg Config // 依赖
}

// New 创建合并器。
// @param cfg 依赖,字段均必填
// @return 就绪的合并器
func New(cfg Config) *Merger { return &Merger{cfg: cfg} }

// MergeExactOnce 扫描并合并全部精确重复组;返回合并掉的冗余 blob 数。
// @Description: 每个 sha256 分组保留最早的 blob,其余引用改指向它、引用计数并入,
// 冗余 blob 打物理删标记(其对象由 sweeper 级联清除)。单组失败只告警、不中断其余组。
// @return 合并失败以外的扫描错误
func (m *Merger) MergeExactOnce(ctx context.Context) (int, error) {
	shas, err := m.cfg.Queries.ListDuplicateSha256(ctx)
	if err != nil {
		return 0, fmt.Errorf("扫描精确重复: %w", err)
	}
	total := 0
	for _, sha := range shas {
		n, err := m.mergeGroup(ctx, sha)
		if err != nil {
			m.cfg.Logger.Warn("精确重复合并失败", "sha256", sha, "err", err)
			continue
		}
		total += n
	}
	if total > 0 {
		m.cfg.Logger.Info("精确重复自动合并", "merged", total)
	}
	return total, nil
}

// mergeGroup 在单事务内合并一个 sha256 分组:字节完全一致,合并零风险。
// @param sha 分组的 SHA256
// @return 合并掉的冗余 blob 数
// @return 事务失败时的错误
func (m *Merger) mergeGroup(ctx context.Context, sha string) (int, error) {
	blobs, err := m.cfg.Queries.ListBlobsBySha256(ctx, sha)
	if err != nil {
		return 0, fmt.Errorf("取分组成员: %w", err)
	}
	if len(blobs) < 2 {
		return 0, nil // 竞态下已被其他轮合并
	}
	keeper := blobs[0]
	deleteAfter := sql.NullString{String: sqlc.TimeText(time.Now().Add(m.cfg.Grace)), Valid: true}

	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启合并事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := m.cfg.Queries.WithTx(tx)

	for _, other := range blobs[1:] {
		// 引用改指向 keeper(内容一致,服务哪个物理副本无差别)。
		if err := q.RepointImages(ctx, sqlc.RepointImagesParams{BlobID: keeper.ID, BlobID_2: other.ID}); err != nil {
			return 0, fmt.Errorf("改指向 keeper: %w", err)
		}
		// 冗余 blob 的引用计数并入 keeper。
		if err := q.AddBlobRef(ctx, sqlc.AddBlobRefParams{RefCount: other.RefCount, ID: keeper.ID}); err != nil {
			return 0, fmt.Errorf("并入引用计数: %w", err)
		}
		// 冗余 blob 打物理删标记,其对象由 sweeper 级联清除。
		if err := q.MarkBlobMergedAway(ctx, sqlc.MarkBlobMergedAwayParams{DeleteAfter: deleteAfter, ID: other.ID}); err != nil {
			return 0, fmt.Errorf("标记冗余 blob 待删: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交合并: %w", err)
	}
	return len(blobs) - 1, nil
}

// Run 按 interval 周期合并,直至 ctx 取消;启动即先跑一轮。
// @param interval 扫描间隔
func (m *Merger) Run(ctx context.Context, interval time.Duration) {
	if _, err := m.MergeExactOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.cfg.Logger.Warn("启动查重合并未完成", "err", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.MergeExactOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				m.cfg.Logger.Warn("定期查重合并未完成", "err", err)
			}
		}
	}
}
