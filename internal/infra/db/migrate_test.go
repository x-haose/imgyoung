package db

import (
	"context"
	"path/filepath"
	"testing"

	"ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/sqlite"
)

// openTest 在临时目录建一个空库,生命周期随测试结束回收。
func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// applyN 只应用前 n 个迁移版本,用于构造历史版本的旧库。
func applyN(t *testing.T, d *DB, n int) {
	t.Helper()
	ctx := context.Background()
	c, err := d.Writer().Conn(ctx)
	if err != nil {
		t.Fatalf("获取迁移连接: %v", err)
	}
	defer func() { _ = c.Close() }()
	drv, err := sqlite.Open(c)
	if err != nil {
		t.Fatalf("初始化迁移驱动: %v", err)
	}
	dir, err := embeddedDir()
	if err != nil {
		t.Fatalf("装载迁移目录: %v", err)
	}
	rrw, err := newRevisions(ctx, c)
	if err != nil {
		t.Fatalf("初始化版本表: %v", err)
	}
	ex, err := migrate.NewExecutor(drv, dir, rrw)
	if err != nil {
		t.Fatalf("构造迁移执行器: %v", err)
	}
	if err := ex.ExecuteN(ctx, n); err != nil {
		t.Fatalf("应用前 %d 个迁移: %v", n, err)
	}
}

// queryString 取单行单列字符串结果,便于断言。
func queryString(t *testing.T, d *DB, query string, args ...any) string {
	t.Helper()
	var s string
	if err := d.QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("查询 %q: %v", query, err)
	}
	return s
}

// TestMigrateFresh 全新库:迁移到最新版本、基线数据就位、重复执行幂等。
func TestMigrateFresh(t *testing.T) {
	conn := openTest(t)
	if err := Migrate(conn); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}
	if err := Migrate(conn); err != nil {
		t.Fatalf("重复迁移应幂等: %v", err)
	}
	for _, table := range []string{"users", "tokens", "sessions", "dedup_candidates", "images", "idempotency_keys"} {
		got := queryString(t, conn,
			`SELECT COALESCE((SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?), '')`, table)
		if got != table {
			t.Errorf("表 %s 未创建", table)
		}
	}
	if name := queryString(t, conn, `SELECT name FROM users WHERE id = 'default'`); name != "admin" {
		t.Errorf("内置管理员登录名 = %q, 期望 admin", name)
	}
	if role := queryString(t, conn, `SELECT role FROM users WHERE id = 'default'`); role != "admin" {
		t.Errorf("内置管理员角色 = %q, 期望 admin", role)
	}
}

// TestMigrateDoesNotReviveLocalChannel 验证内置 local 渠道只播种一次:管理员删掉它之后
// 再次迁移(每次启动都会跑)不得把它种回来;渠道表彻底为空的全新库才播种。
func TestMigrateDoesNotReviveLocalChannel(t *testing.T) {
	conn := openTest(t)
	if err := Migrate(conn); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO channels (name, driver) VALUES ('mine', 's3')`); err != nil {
		t.Fatalf("新增渠道: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM channels WHERE name = 'local'`); err != nil {
		t.Fatalf("删除 local 渠道: %v", err)
	}
	if err := Migrate(conn); err != nil {
		t.Fatalf("再次迁移: %v", err)
	}
	if got := queryString(t, conn, `SELECT COUNT(*) FROM channels WHERE name = 'local'`); got != "0" {
		t.Fatalf("已删除的 local 渠道被重新播种(count=%s)", got)
	}
}

// TestMigrateBackfillsLegacyRows 存量库:旧版 schema 中的历史数据经迁移后被正确回填。
func TestMigrateBackfillsLegacyRows(t *testing.T) {
	conn := openTest(t)
	// 只应用前两个版本,构造多用户改造前的旧库:default 用户旧登录名、
	// images.user_id 可空、幂等键无用户维度。
	applyN(t, conn, 2)
	for _, stmt := range []string{
		`INSERT INTO users (id, name) VALUES ('default', 'default')`,
		`INSERT INTO blobs (sha256, phash, storage_key, mime, size, width, height)
		 VALUES ('aa', 1, 'k_aa', 'image/png', 10, 1, 1)`,
		`INSERT INTO images (id, blob_id, user_id) VALUES ('img1', 1, NULL)`,
		`INSERT INTO idempotency_keys (key, image_id) VALUES ('k1', 'img1')`,
	} {
		if _, err := conn.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("写入旧版数据 %q: %v", stmt, err)
		}
	}
	if err := Migrate(conn); err != nil {
		t.Fatalf("升级迁移: %v", err)
	}
	if got := queryString(t, conn, `SELECT user_id FROM images WHERE id = 'img1'`); got != "default" {
		t.Errorf("images.user_id 回填 = %q, 期望 default", got)
	}
	if got := queryString(t, conn, `SELECT user_id FROM idempotency_keys WHERE key = 'k1'`); got != "default" {
		t.Errorf("idempotency_keys.user_id 回填 = %q, 期望 default", got)
	}
	if got := queryString(t, conn, `SELECT name || '/' || role FROM users WHERE id = 'default'`); got != "admin/admin" {
		t.Errorf("内置管理员升级 = %q, 期望 admin/admin", got)
	}
	// 表重建后外键关系必须完好。
	var n int
	if err := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil {
		t.Fatalf("外键完整性检查: %v", err)
	}
	if n != 0 {
		t.Errorf("外键完整性违例 %d 处", n)
	}
}
