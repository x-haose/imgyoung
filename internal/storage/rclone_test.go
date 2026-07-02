package storage

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestRcloneDriverRoundTrip 验证 Put → Stat → Open → Delete 全链路行为正确。
// @Description: 使用 rclone local backend + 临时目录,零外部依赖(见设计定稿·测试策略)。
func TestRcloneDriverRoundTrip(t *testing.T) {
	ctx := t.Context()
	d, err := NewRcloneDriver(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("NewRcloneDriver 返回错误: %v", err)
	}

	const key = "ab/test.bin"
	payload := []byte("hello imgyoung")

	if err := d.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("Put 返回错误: %v", err)
	}

	info, err := d.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat 返回错误: %v", err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("Stat.Size = %d, 期望 %d", info.Size, len(payload))
	}

	rc, err := d.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open 返回错误: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("Close 返回错误: %v", cerr)
	}
	if err != nil {
		t.Fatalf("读取对象内容返回错误: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("读回内容 = %q, 期望 %q", got, payload)
	}

	if err := d.Delete(ctx, key); err != nil {
		t.Fatalf("Delete 返回错误: %v", err)
	}
	if _, err := d.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 Stat 错误 = %v, 期望 ErrNotFound", err)
	}
}

// TestRcloneDriverOpenMissing 验证读取不存在的对象返回 ErrNotFound 哨兵错误。
func TestRcloneDriverOpenMissing(t *testing.T) {
	ctx := t.Context()
	d, err := NewRcloneDriver(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("NewRcloneDriver 返回错误: %v", err)
	}
	if _, err := d.Open(ctx, "no/such/object"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open 缺失对象错误 = %v, 期望 ErrNotFound", err)
	}
}
