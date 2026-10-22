package replication

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// fixed 返回固定上限的取值函数。
func fixed(bytesPerSec int64) func() int64 { return func() int64 { return bytesPerSec } }

// TestThrottledReaderUnlimitedSkipsBucket 上限为 0 表示不限:读取不经过令牌桶,即使 ctx 已取消也读得出来。
func TestThrottledReaderUnlimitedSkipsBucket(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	payload := bytes.Repeat([]byte("x"), 2<<20)
	got, err := io.ReadAll(newBandwidth().wrap(ctx, bytes.NewReader(payload), fixed(0)))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("不限速应原样读出: err=%v, len=%d", err, len(got))
	}
}

// TestThrottledReaderPreservesBytes 限速不改变内容:读出的字节与源完全一致。
func TestThrottledReaderPreservesBytes(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 16<<10) // 256 KiB
	r := newBandwidth().wrap(t.Context(), bytes.NewReader(payload), fixed(64<<20))
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("限速读出的内容与源不一致: err=%v, len=%d", err, len(got))
	}
}

// TestThrottledReaderStopsOnCancel 等待额度时 ctx 已取消,读取返回取消错误,不会一直等下去。
func TestThrottledReaderStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := newBandwidth().wrap(ctx, bytes.NewReader(make([]byte, 1<<20)), fixed(1<<20))
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("ctx 已取消,期望读取返回错误")
	}
}

// TestThrottledReaderFollowsLiveLimit 限速随配置热改:在途读取下一次读取就按新值走,改回 0 即不再等待。
func TestThrottledReaderFollowsLiveLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	limit := int64(64 << 20)
	r := newBandwidth().wrap(ctx, bytes.NewReader(bytes.Repeat([]byte("y"), 2<<20)), func() int64 { return limit })
	buf := make([]byte, 4096)
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("限速内的读取失败: %v", err)
	}
	limit = 0
	cancel()
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("改回不限速后读取不应再等待额度,得 %v", err)
	}
}
