package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCheckSnapshotRejectsIncompleteFiles 完整的快照通过检查;截断的库与 0 字节文件(写快照途中进程被杀留下的
// 那两种)都判为不完整——0 字节文件在 SQLite 看来是合法的空库,换入后会被迁移建成一个全新实例。
func TestCheckSnapshotRejectsIncompleteFiles(t *testing.T) {
	d := openTest(t)
	if err := Migrate(d); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	snap := filepath.Join(t.TempDir(), "snap.db")
	if _, err := d.ExecContext(context.Background(), `VACUUM INTO ?`, snap); err != nil {
		t.Fatalf("生成快照: %v", err)
	}
	if err := CheckSnapshot(t.Context(), snap); err != nil {
		t.Fatalf("完整快照未通过检查: %v", err)
	}
	info, err := os.Stat(snap)
	if err != nil {
		t.Fatalf("读取快照大小: %v", err)
	}
	if err := os.Truncate(snap, info.Size()/3); err != nil {
		t.Fatalf("截断快照: %v", err)
	}
	if err := CheckSnapshot(t.Context(), snap); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("截断的快照应判为不完整,得 %v", err)
	}
	if err := os.Truncate(snap, 0); err != nil {
		t.Fatalf("清空快照: %v", err)
	}
	if err := CheckSnapshot(t.Context(), snap); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("0 字节的快照应判为不完整,得 %v", err)
	}
}
