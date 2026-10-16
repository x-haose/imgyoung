package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// seedDefaults 播种系统开箱运行所需的基线数据:内置管理员与内置 local 渠道。
// @Description: 每次迁移后执行,重复执行无副作用。内置管理员按固定 id 幂等插入,
// 登录名为 admin,密码留空即禁止登录,首启由 seedAdminPassword 设置初始密码。
// local 渠道只在全新库播种(取得 id=1),与内置管理员同一事务:内置管理员不可删除,本次插入了它才是全新库。
// 被删除或改名的 local 不会在重启时复活,渠道表被删空也不会。
// @param ctx 控制写入超时与取消
// @param conn 执行迁移的连接
// @return 播种失败时的错误
func seedDefaults(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启播种事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO users (id, name, role) VALUES (?, 'admin', 'admin')`, sqlc.DefaultUserID)
	if err != nil {
		return fmt.Errorf("播种内置管理员: %w", err)
	}
	fresh, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("播种内置管理员: %w", err)
	}
	if fresh == 1 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO channels (name, driver, priority) VALUES ('local', 'local', 100)`); err != nil {
			return fmt.Errorf("播种 local 渠道: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交播种事务: %w", err)
	}
	return nil
}
