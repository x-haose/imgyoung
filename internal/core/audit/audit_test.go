package audit_test

import (
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// TestRecordAppendsRow 验证 Record 写入的字段完整落库。
func TestRecordAppendsRow(t *testing.T) {
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

	rec := audit.New(sqlc.New(conn))
	if err := rec.Record(t.Context(), "default", audit.ActionChannelCreate, "r2", "id=2 encrypted=true"); err != nil {
		t.Fatalf("Record 返回错误: %v", err)
	}

	var actor, action, target, detail string
	row := conn.QueryRowContext(t.Context(),
		"SELECT actor, action, target, detail FROM audit_log ORDER BY id DESC LIMIT 1")
	if err := row.Scan(&actor, &action, &target, &detail); err != nil {
		t.Fatalf("读取审计记录: %v", err)
	}
	if actor != "default" || action != audit.ActionChannelCreate || target != "r2" || detail != "id=2 encrypted=true" {
		t.Fatalf("审计记录字段不符: actor=%q action=%q target=%q detail=%q", actor, action, target, detail)
	}
}
