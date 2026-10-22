package replication

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// TestBandwidthUnlimitedPassesThrough 上限为 0 表示不限:原样返回读取流,不引入任何包装。
func TestBandwidthUnlimitedPassesThrough(t *testing.T) {
	src := bytes.NewReader([]byte("x"))
	if got := newBandwidth().wrap(t.Context(), src, 0); got != io.Reader(src) {
		t.Fatalf("上限为 0 应原样返回读取流,得 %T", got)
	}
}

// TestThrottledReaderPreservesBytes 限速不改变内容:读出的字节与源完全一致,单次读取不超过突发量。
func TestThrottledReaderPreservesBytes(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 16<<10) // 256 KiB
	r := newBandwidth().wrap(t.Context(), bytes.NewReader(payload), 64<<20)
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("限速读出的内容与源不一致: err=%v, len=%d", err, len(got))
	}
}

// TestThrottledReaderStopsOnCancel 等待额度时 ctx 已取消,读取返回取消错误,不会一直等下去。
func TestThrottledReaderStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := newBandwidth().wrap(ctx, bytes.NewReader(make([]byte, 1<<20)), 1<<20)
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("ctx 已取消,期望读取返回错误")
	}
}
