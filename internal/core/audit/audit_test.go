package audit_test

import (
	"testing"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// TestRecordAppendsRow 验证 Record 写入的字段完整落库(经生产读路径 ListAuditLog 回读)。
func TestRecordAppendsRow(t *testing.T) {
	_, q := dbtest.New(t)

	rec := audit.New(q)
	if err := rec.Record(t.Context(), "default", audit.ActionChannelCreate, "r2", "id=2 encrypted=true"); err != nil {
		t.Fatalf("Record 返回错误: %v", err)
	}

	rows, err := q.ListAuditLog(t.Context(), sqlc.ListAuditLogParams{
		Actor: "", Action: "", Target: "", Since: "", Until: "", PageLimit: 1,
	})
	if err != nil {
		t.Fatalf("读取审计记录: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("审计记录数 = %d, 期望 1", len(rows))
	}
	r := rows[0]
	if r.Actor != "default" || r.Action != audit.ActionChannelCreate || r.Target != "r2" || r.Detail != "id=2 encrypted=true" {
		t.Fatalf("审计记录字段不符: actor=%q action=%q target=%q detail=%q", r.Actor, r.Action, r.Target, r.Detail)
	}
}
