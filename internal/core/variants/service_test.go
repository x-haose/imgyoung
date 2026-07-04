package variants_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// pngBytes 合成一张小 PNG 作源图。
func pngBytes(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			m.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatalf("合成 PNG: %v", err)
	}
	return buf.Bytes()
}

// newService 基于 newStore 构造一个按需处理服务。
func newService(t *testing.T, workers int, timeout time.Duration) (*variants.Service, int64) {
	t.Helper()
	store, blobID, _ := newStore(t, 1<<30)
	set := imaging.EncodeSettings{
		QualityWebP: 80, QualityJPEG: 85, WebPMethod: 4, MaxPixels: 50_000_000,
	}
	svc := variants.NewService(variants.ServiceConfig{
		Store: store, Proc: imaging.NewProcessor(set), Settings: set,
		Workers: workers, Timeout: timeout, Logger: slog.New(slog.DiscardHandler),
	})
	return svc, blobID
}

func TestServiceProcessesThenCaches(t *testing.T) {
	svc, blobID := newService(t, 4, 3*time.Second)
	ctx := t.Context()
	src := pngBytes(t)

	var calls atomic.Int32
	srcFn := func(context.Context) ([]byte, error) { calls.Add(1); return src, nil }
	params := imaging.ProcessParams{Width: 32, Format: imaging.FormatWebP}

	data, mime, err := svc.GetOrProcess(ctx, blobID, params, srcFn)
	if err != nil {
		t.Fatalf("首次处理: %v", err)
	}
	if len(data) == 0 || mime != "image/webp" {
		t.Fatalf("产物异常: len=%d mime=%q", len(data), mime)
	}
	// 二次访问应命中缓存,不再拉源图。
	if _, _, err := svc.GetOrProcess(ctx, blobID, params, srcFn); err != nil {
		t.Fatalf("二次访问: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("srcFn 调用 %d 次,期望 1(二次走缓存)", got)
	}
}

func TestServiceSingleFlightMergesConcurrent(t *testing.T) {
	svc, blobID := newService(t, 4, 3*time.Second)
	ctx := t.Context()
	src := pngBytes(t)

	var calls atomic.Int32
	release := make(chan struct{})
	//nolint:unparam // 须匹配 srcFn 回调签名 func(ctx)([]byte,error);测试替身不产生错误
	srcFn := func(context.Context) ([]byte, error) {
		calls.Add(1)
		<-release // 卡住首个处理,给并发调用汇聚的时间窗
		return src, nil
	}
	params := imaging.ProcessParams{Width: 32, Format: imaging.FormatWebP}

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = svc.GetOrProcess(ctx, blobID, params, srcFn)
		}()
	}
	time.Sleep(50 * time.Millisecond) // 让并发调用都进入 singleflight
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("srcFn 调用 %d 次,期望 1(singleflight 合并同键并发)", got)
	}
}

func TestServiceBusyReturnsErrBusy(t *testing.T) {
	// 单槽 + 极短超时:一个处理占住槽,另一个不同键请求抢不到即 ErrBusy。
	svc, blobID := newService(t, 1, 80*time.Millisecond)
	ctx := t.Context()
	src := pngBytes(t)

	hold := make(chan struct{})
	//nolint:unparam // 须匹配 srcFn 回调签名 func(ctx)([]byte,error);测试替身不产生错误
	blockingSrc := func(context.Context) ([]byte, error) { <-hold; return src, nil }
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = svc.GetOrProcess(ctx, blobID, imaging.ProcessParams{Width: 32, Format: imaging.FormatWebP}, blockingSrc)
	}()
	time.Sleep(30 * time.Millisecond) // 确保占槽者已入临界区

	_, _, err := svc.GetOrProcess(ctx, blobID, imaging.ProcessParams{Width: 64, Format: imaging.FormatWebP},
		func(context.Context) ([]byte, error) { return src, nil })
	if !errors.Is(err, variants.ErrBusy) {
		t.Fatalf("并发满时应返回 ErrBusy,得到: %v", err)
	}
	close(hold)
	wg.Wait()
}
