package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// flushStopTimeout 停机前最后一次 flush 的超时预算。
const flushStopTimeout = 5 * time.Second

// Tracker 访问活跃度追踪:内存聚合 + 周期批量落库,
// 避免逐请求 UPDATE 与 SQLite 单写者争锁。并发安全。
type Tracker struct {
	conn    *sql.DB             // 数据库连接(批量事务用)
	queries *sqlc.Queries       // sqlc 查询集
	logger  *slog.Logger        // 结构化日志
	mu      sync.Mutex          // 保护 touched
	touched map[string]struct{} // 自上次 flush 以来被访问的引用 id 集合
}

// NewTracker 创建活跃度追踪器。
// @param conn 数据库连接
// @param queries 查询集
// @param logger 日志
// @return 就绪的追踪器
func NewTracker(conn *sql.DB, queries *sqlc.Queries, logger *slog.Logger) *Tracker {
	return &Tracker{conn: conn, queries: queries, logger: logger, touched: make(map[string]struct{})}
}

// Touch 记录一次公开访问(仅写内存,极轻量;真正落库由 flush 批量完成)。
// @param imageID 被访问的引用 id
func (t *Tracker) Touch(imageID string) {
	t.mu.Lock()
	t.touched[imageID] = struct{}{}
	t.mu.Unlock()
}

// Flush 把累积的访问批量落库:更新 last_public_access_at 并摘除 pending_cleanup 标签。
// @Description: 失败时把这批放回,留待下次重试——活跃度丢失只降冷图判定精度,不影响正确性。
// @return 落库失败时的错误
func (t *Tracker) Flush(ctx context.Context) error {
	t.mu.Lock()
	if len(t.touched) == 0 {
		t.mu.Unlock()
		return nil
	}
	batch := t.touched
	t.touched = make(map[string]struct{})
	t.mu.Unlock()

	now := sql.NullString{String: sqlc.TimeText(time.Now()), Valid: true}
	tx, err := t.conn.BeginTx(ctx, nil)
	if err != nil {
		t.requeue(batch)
		return fmt.Errorf("开启活跃度事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := t.queries.WithTx(tx)
	for id := range batch {
		if err := q.TouchImageAccess(ctx, sqlc.TouchImageAccessParams{LastPublicAccessAt: now, ID: id}); err != nil {
			t.requeue(batch)
			return fmt.Errorf("更新活跃度: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.requeue(batch)
		return fmt.Errorf("提交活跃度: %w", err)
	}
	return nil
}

// Run 按 interval 周期 flush,直至 ctx 取消;停机前做最后一次 flush 不丢累积。
// @param interval flush 间隔
func (t *Tracker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), flushStopTimeout)
			if err := t.Flush(flushCtx); err != nil {
				t.logger.Warn("停机活跃度 flush 未完成", "err", err)
			}
			cancel()
			return
		case <-ticker.C:
			if err := t.Flush(ctx); err != nil && !errors.Is(err, context.Canceled) {
				t.logger.Warn("活跃度 flush 未完成", "err", err)
			}
		}
	}
}

// requeue 把一批 id 放回待 flush 集合(落库失败时的重试兜底)。
func (t *Tracker) requeue(batch map[string]struct{}) {
	t.mu.Lock()
	for id := range batch {
		t.touched[id] = struct{}{}
	}
	t.mu.Unlock()
}
