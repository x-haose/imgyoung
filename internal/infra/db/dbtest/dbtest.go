// Package dbtest 提供测试用的临时 SQLite 库装配,消除各包测试里重复的"打开→迁移→建查询集"样板。
package dbtest

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// New 打开并迁移一个临时目录下的 SQLite 库,返回连接与查询集;库随测试结束自动关闭。
// @Description: 迁移会顺带播种内置 default 用户与 local 渠道(见 db.Migrate)。
// @param t 测试上下文,失败即 Fatal,连接经 t.Cleanup 关闭
// @return 数据库连接
// @return sqlc 查询集
func New(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() {
		if cerr := conn.Close(); cerr != nil {
			t.Errorf("关闭测试数据库: %v", cerr)
		}
	})
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	return conn, sqlc.New(conn)
}
