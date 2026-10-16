package db

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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

// TestMigrateDoesNotReviveLocalChannel 验证内置 local 渠道只在全新库播种一次:它是唯一渠道时被删掉、
// 渠道表为空,再次迁移(每次启动都会跑)也不得把它种回来。
func TestMigrateDoesNotReviveLocalChannel(t *testing.T) {
	conn := openTest(t)
	if err := Migrate(conn); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `DELETE FROM channels WHERE name = 'local'`); err != nil {
		t.Fatalf("删除 local 渠道: %v", err)
	}
	if err := Migrate(conn); err != nil {
		t.Fatalf("再次迁移: %v", err)
	}
	if got := queryString(t, conn, `SELECT COUNT(*) FROM channels`); got != "0" {
		t.Fatalf("已删除的 local 渠道被重新播种(count=%s)", got)
	}
}

// TestMigrateRejectsNewerSchema 库被更新版本的二进制迁移过(记有本二进制不认识的版本):拒绝启动,
// 错误里有库里的最新版本。
func TestMigrateRejectsNewerSchema(t *testing.T) {
	conn := openTest(t)
	if err := Migrate(conn); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}
	const future = "29991231235959"
	if _, err := conn.ExecContext(context.Background(), `INSERT INTO `+revisionsTable+
		` (version, description, type, applied, total, executed_at, execution_time, hash)
		VALUES (?, 'future', 2, 1, 1, '2999-12-31T23:59:59Z', 0, '')`, future); err != nil {
		t.Fatalf("写入未来版本记录: %v", err)
	}
	err := Migrate(conn)
	if err == nil || !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), future) {
		t.Fatalf("迁移错误 = %v, 期望 ErrSchemaNewer 且指出库里的版本 %s", err, future)
	}
}
