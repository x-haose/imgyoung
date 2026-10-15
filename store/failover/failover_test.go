package failover_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"
	"github.com/x-haose/imgyoung/store/rclone"
)

// openLocal 打开一个以 path 为根的本地驱动,测试结束时关闭。
func openLocal(t *testing.T, path string) driver.Driver {
	t.Helper()
	d, err := rclone.Open(t.Context(), rclone.Options{Backend: "local", Path: path})
	if err != nil {
		t.Fatalf("打开本地驱动: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

// brokenLocal 返回一个根落在普通文件之下的本地驱动:读写都以非"不存在"的错误失败。
func brokenLocal(t *testing.T) driver.Driver {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("写占位文件: %v", err)
	}
	return openLocal(t, filepath.Join(blocker, "data"))
}

// TestWriteInsufficient 份数要求超过可写驱动数时,试完全部驱动后报份数不足,尝试记录完整。
func TestWriteInsufficient(t *testing.T) {
	ds := []driver.Driver{brokenLocal(t), openLocal(t, t.TempDir())}
	data := []byte("x")
	attempts, err := failover.Write(t.Context(), ds, "k", bytes.NewReader(data), 1, failover.WriteOptions{Need: 2})
	if !errors.Is(err, failover.ErrInsufficient) {
		t.Fatalf("错误 = %v, 期望 ErrInsufficient", err)
	}
	if len(attempts) != 2 || attempts[0].Err == nil || attempts[1].Err != nil {
		t.Fatalf("尝试记录 = %+v, 期望第 0 个失败、第 1 个成功", attempts)
	}
}

// TestOpenFailureKinds 读取全部失败时区分"处处不存在"与"有驱动故障"。
func TestOpenFailureKinds(t *testing.T) {
	healthy := []driver.Driver{openLocal(t, t.TempDir()), openLocal(t, t.TempDir())}
	_, _, err := failover.Open(t.Context(), healthy, "missing")
	if !errors.Is(err, driver.ErrNotFound) || errors.Is(err, failover.ErrUnavailable) {
		t.Fatalf("全部不存在时错误 = %v, 期望只命中 driver.ErrNotFound", err)
	}

	mixed := []driver.Driver{brokenLocal(t), openLocal(t, t.TempDir())}
	_, attempts, err := failover.Open(t.Context(), mixed, "missing")
	if !errors.Is(err, failover.ErrUnavailable) {
		t.Fatalf("含故障驱动时错误 = %v, 期望 ErrUnavailable", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("尝试次数 = %d, 期望 2", len(attempts))
	}
}
