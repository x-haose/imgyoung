package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// seedDefaults 播种系统开箱运行所需的基线数据:内置管理员与内置 local 渠道。
// @Description: 幂等(INSERT OR IGNORE),每次迁移后执行;全新库据此开箱即用,
// 已初始化的库重复执行无副作用。内置管理员登录名为 admin,密码留空即禁止登录,
// 首启经环境变量初始化。local 渠道在全新库中取得 id=1。
// @param ctx 控制写入超时与取消
// @param conn 已迁移的数据库连接
// @return 播种失败时的错误
func seedDefaults(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO users (id, name, role) VALUES (?, 'admin', 'admin')`, sqlc.DefaultUserID); err != nil {
		return fmt.Errorf("播种内置管理员: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO channels (name, driver, priority) VALUES ('local', 'local', 100)`); err != nil {
		return fmt.Errorf("播种 local 渠道: %w", err)
	}
	return nil
}
