package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestQuickCheckRejectsTruncatedFile 完整的库通过检查;截断的库(写快照途中进程被杀留下的那种)判为损坏。
func TestQuickCheckRejectsTruncatedFile(t *testing.T) {
	d := openTest(t)
	if err := Migrate(d); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	snap := filepath.Join(t.TempDir(), "snap.db")
	if _, err := d.ExecContext(context.Background(), `VACUUM INTO ?`, snap); err != nil {
		t.Fatalf("生成快照: %v", err)
	}
	if err := QuickCheck(t.Context(), snap); err != nil {
		t.Fatalf("完整快照未通过检查: %v", err)
	}
	info, err := os.Stat(snap)
	if err != nil {
		t.Fatalf("读取快照大小: %v", err)
	}
	if err := os.Truncate(snap, info.Size()/3); err != nil {
		t.Fatalf("截断快照: %v", err)
	}
	if err := QuickCheck(t.Context(), snap); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("截断的快照应判为损坏,得 %v", err)
	}
}
