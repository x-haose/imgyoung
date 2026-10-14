package db

import (
	"context"
	"fmt"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// seedDefaults 播种系统开箱运行所需的基线数据:内置管理员与内置 local 渠道。
// @Description: 每次迁移后执行,重复执行无副作用。内置管理员按固定 id 幂等插入,
// 登录名为 admin,密码留空即禁止登录,首启由 seedAdminPassword 播种出厂默认密码。
// local 渠道只在渠道表为空的全新库播种(取得 id=1):渠道表非空说明管理员已经接管了
// 渠道配置,被删除或改名的 local 不能在每次启动时复活。
// @param ctx 控制写入超时与取消
// @param conn 执行迁移的连接
// @return 播种失败时的错误
func seedDefaults(ctx context.Context, conn sqlConn) error {
	if _, err := conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO users (id, name, role) VALUES (?, 'admin', 'admin')`, sqlc.DefaultUserID); err != nil {
		return fmt.Errorf("播种内置管理员: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO channels (name, driver, priority)
		 SELECT 'local', 'local', 100
		 WHERE NOT EXISTS (SELECT 1 FROM channels)`); err != nil {
		return fmt.Errorf("播种 local 渠道: %w", err)
	}
	return nil
}
