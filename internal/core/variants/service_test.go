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
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// testSettings 构造运营配置读取口:内嵌默认值上按需覆写(mod 为 nil 即取默认)。
// @param mod 覆写钩子,按需改动默认配置副本
// @return 逐请求取值的配置读取口
func testSettings(t *testing.T, mod func(*config.Settings)) func() config.Settings {
	t.Helper()
	set := config.Defaults()
	if mod != nil {
		mod(&set)
	}
	return func() config.Settings { return set }
}

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

// newService 基于 newStore 构造一个按需处理服务,并发上限与超时按参数覆写默认配置。
func newService(t *testing.T, workers int, timeout time.Duration) (*variants.Service, int64) {
	t.Helper()
	env := newStore(t, 1<<30)
	settings := testSettings(t, func(s *config.Settings) {
		s.ProcessWorkers = workers
		s.ProcessTimeoutMS = int(timeout / time.Millisecond)
	})
	svc := variants.NewService(variants.ServiceConfig{
		Store: env.store, Settings: settings, Logger: slog.New(slog.DiscardHandler),
	})
	return svc, env.blobID
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
	for range n {
		wg.Go(func() {
			_, _, _ = svc.GetOrProcess(ctx, blobID, params, srcFn)
		})
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
	entered := make(chan struct{})
	// 源图拉取发生在抢到处理槽之后:它被调用即证明占槽者已在临界区,比固定睡眠可靠。
	//nolint:unparam // 须匹配 srcFn 回调签名 func(ctx)([]byte,error);测试替身不产生错误
	blockingSrc := func(context.Context) ([]byte, error) { close(entered); <-hold; return src, nil }
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _, _ = svc.GetOrProcess(ctx, blobID, imaging.ProcessParams{Width: 32, Format: imaging.FormatWebP},
			blockingSrc)
	})
	<-entered

	_, _, err := svc.GetOrProcess(ctx, blobID, imaging.ProcessParams{Width: 64, Format: imaging.FormatWebP},
		func(context.Context) ([]byte, error) { return src, nil })
	if !errors.Is(err, variants.ErrBusy) {
		t.Fatalf("并发满时应返回 ErrBusy,得到: %v", err)
	}
	close(hold)
	wg.Wait()
}
