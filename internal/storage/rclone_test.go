package storage

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
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

// TestRcloneDriverList 验证递归列举:返回全部对象的键/大小/修改时间,空目录返回空。
func TestRcloneDriverList(t *testing.T) {
	ctx := t.Context()
	d, err := NewRcloneDriver(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("NewRcloneDriver 返回错误: %v", err)
	}

	empty, err := d.List(ctx, "")
	if err != nil {
		t.Fatalf("列举空渠道: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空渠道列举出 %d 个对象", len(empty))
	}

	before := time.Now().Add(-time.Minute)
	for _, key := range []string{"a.bin", "sub/b.bin", "_imgyoung/backup/c.db"} {
		payload := []byte("x-" + key)
		if err := d.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
			t.Fatalf("写入 %s: %v", key, err)
		}
	}

	all, err := d.List(ctx, "")
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("对象数 = %d, 期望 3(递归含子目录)", len(all))
	}
	seen := map[string]ObjectInfo{}
	for _, o := range all {
		seen[o.Key] = o
	}
	if o, ok := seen["sub/b.bin"]; !ok || o.Size != int64(len("x-sub/b.bin")) {
		t.Fatalf("子目录对象缺失或大小错误: %+v", seen)
	}
	if seen["a.bin"].ModTime.Before(before) {
		t.Fatalf("ModTime 未填充: %v", seen["a.bin"].ModTime)
	}

	// 按目录前缀列举:只返回该目录下的对象。
	backups, err := d.List(ctx, "_imgyoung/backup")
	if err != nil {
		t.Fatalf("按前缀列举: %v", err)
	}
	if len(backups) != 1 || backups[0].Key != "_imgyoung/backup/c.db" {
		t.Fatalf("前缀列举结果 = %+v, 期望仅 c.db 且键为完整路径", backups)
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
