package router_test

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/config"
)

// setupVariants 组装一个启用按需处理的测试服务,返回服务与 variant 文件目录(供断言缓存状态)。
// @param procMaxPixelsMP 处理引擎像素预算(百万像素,独立于上传预算);
// 置 0 可强制任何源图超预算、处理失败,以测降级回退。
func setupVariants(t *testing.T, procMaxPixelsMP int64) (*httptest.Server, string) {
	t.Helper()
	var vdir string
	ts := newTestServer(t, func(tc *testConfig, env testEnv) {
		discard := slog.New(slog.DiscardHandler)
		vdir = filepath.Join(env.Dir, "variants")
		applySettings(t, env.Store, func(s *config.Settings) {
			s.ProcessWidths = []int{100, 200, 400}
		})
		procSet := env.Store.Current()
		procSet.MaxPixelsMP = procMaxPixelsMP
		store := variants.New(variants.Config{
			Dir: vdir, Capacity: func() int64 { return 1 << 30 }, Queries: env.Queries, Logger: discard,
		})
		tc.Public.Variants = variants.NewService(variants.ServiceConfig{
			Store:    store,
			Settings: func() config.Settings { return procSet },
			Logger:   discard,
		})
	})
	return ts, vdir
}

// bigPNG 生成一张 300×200 的真实 PNG,供缩放/处理验证(宽于最小档位 200)。
func bigPNG(t *testing.T) []byte {
	t.Helper()
	const w, h = 300, 200
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatalf("合成 PNG: %v", err)
	}
	return buf.Bytes()
}

// gifBytes 生成一张小 GIF,用于验证 gif 源不被处理。
func gifBytes(t *testing.T) []byte {
	t.Helper()
	m := image.NewPaletted(image.Rect(0, 0, 100, 80), palette.Plan9)
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			m.SetColorIndex(x, y, uint8((x+y)%256))
		}
	}
	var buf bytes.Buffer
	if err := gif.Encode(&buf, m, nil); err != nil {
		t.Fatalf("合成 GIF: %v", err)
	}
	return buf.Bytes()
}

// countFiles 统计目录下的普通文件数(递归),用于断言缓存落盘条数。
func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("统计缓存文件: %v", err)
	}
	return n
}

// TestServeVariantResizeAndCache 验收:?w=200&format=webp 生效、缩放正确、二次访问命中缓存(不新增文件)。
func TestServeVariantResizeAndCache(t *testing.T) {
	ts, vdir := setupVariants(t, 50)

	up := uploadReq(t, ts, testToken, "a.png", bigPNG(t), nil)
	defer closeBody(t, up)
	id, _ := decodeUpload(t, up)

	getVariant := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/i/"+id+"?w=200&format=webp", nil)
		if err != nil {
			t.Fatalf("构造请求: %v", err)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("访问衍生图: %v", err)
		}
		return resp
	}

	first := getVariant()
	defer closeBody(t, first)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("首次状态码 = %d, 期望 200", first.StatusCode)
	}
	if ct := first.Header.Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("Content-Type = %q, 期望 image/webp", ct)
	}
	// 显式 ?format=webp 的产物与 Accept 无关,不应设 Vary,避免污染 CDN 缓存键。
	if v := first.Header.Get("Vary"); v != "" {
		t.Fatalf("显式 format 请求 Vary = %q, 期望空", v)
	}
	data, err := io.ReadAll(first.Body)
	if err != nil {
		t.Fatalf("读响应: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("解码衍生图: %v", err)
	}
	if cfg.Width != 200 {
		t.Fatalf("衍生图宽 = %d, 期望 200", cfg.Width)
	}

	if got := countFiles(t, vdir); got != 1 {
		t.Fatalf("首次处理后缓存文件数 = %d, 期望 1", got)
	}
	// 二次访问同参:命中缓存,不新增文件。
	second := getVariant()
	defer closeBody(t, second)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("二次状态码 = %d, 期望 200", second.StatusCode)
	}
	if got := countFiles(t, vdir); got != 1 {
		t.Fatalf("二次访问后缓存文件数 = %d, 期望仍为 1(命中缓存)", got)
	}
}

// TestServeVariantRejectsBadTier 验收:档外宽度/格式返回 400。
func TestServeVariantRejectsBadTier(t *testing.T) {
	ts, _ := setupVariants(t, 50)
	up := uploadReq(t, ts, testToken, "a.png", bigPNG(t), nil)
	defer closeBody(t, up)
	id, _ := decodeUpload(t, up)

	for _, q := range []string{"?w=333", "?format=tiff", "?w=999&format=webp"} {
		resp, err := ts.Client().Get(ts.URL + "/i/" + id + q)
		if err != nil {
			t.Fatalf("请求 %s: %v", q, err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("档外参数 %s 状态码 = %d, 期望 400", q, resp.StatusCode)
		}
		closeBody(t, resp)
	}
}

// TestServeVariantNegotiatesFormat 验收:Accept: image/webp 时无 format 参数也协商为 webp。
func TestServeVariantNegotiatesFormat(t *testing.T) {
	ts, _ := setupVariants(t, 50)
	up := uploadReq(t, ts, testToken, "a.png", bigPNG(t), nil)
	defer closeBody(t, up)
	id, _ := decodeUpload(t, up)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/i/"+id+"?w=200", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Accept", "image/webp,*/*")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("访问: %v", err)
	}
	defer closeBody(t, resp)
	if ct := resp.Header.Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("协商 Content-Type = %q, 期望 image/webp", ct)
	}
	// 协商路径响应随 Accept 变化,必须 Vary: Accept 让 CDN 按 Accept 分缓存键。
	if v := resp.Header.Get("Vary"); v != "Accept" {
		t.Fatalf("协商路径 Vary = %q, 期望 Accept", v)
	}
}

// TestServeVariantGifNotProcessed 验收:gif 源即便显式请求处理参数也只发原图(不解码首帧转静图、动画不丢)。
func TestServeVariantGifNotProcessed(t *testing.T) {
	ts, vdir := setupVariants(t, 50)
	up := uploadReq(t, ts, testToken, "a.gif", gifBytes(t), nil)
	defer closeBody(t, up)
	id, _ := decodeUpload(t, up)

	// 显式 ?w=200&format=webp:gif 源仍应原样返回,不被处理。
	resp, err := ts.Client().Get(ts.URL + "/i/" + id + "?w=200&format=webp")
	if err != nil {
		t.Fatalf("访问: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/gif" {
		t.Fatalf("Content-Type = %q, 期望 image/gif(gif 不处理,发原图)", ct)
	}
	if got := countFiles(t, vdir); got != 0 {
		t.Fatalf("gif 请求后缓存文件数 = %d, 期望 0(未处理未落缓存)", got)
	}
}

// TestServeVariantDegradedFallbackNoStore 验收:处理失败时降级回退原图,且响应 no-store 不被当衍生图缓存(防投毒)。
func TestServeVariantDegradedFallbackNoStore(t *testing.T) {
	// 处理引擎像素预算置极小(100),300x200 源必超预算 → 处理失败 → 降级回退;但原图服务不解码,照常可用。
	ts, vdir := setupVariants(t, 0)
	up := uploadReq(t, ts, testToken, "a.png", bigPNG(t), nil)
	defer closeBody(t, up)
	id, _ := decodeUpload(t, up)

	resp, err := ts.Client().Get(ts.URL + "/i/" + id + "?w=200&format=webp")
	if err != nil {
		t.Fatalf("访问: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("降级状态码 = %d, 期望 200", resp.StatusCode)
	}
	// 回退发的是全尺寸原 png,不是 webp 衍生图。
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("降级 Content-Type = %q, 期望 image/png(全尺寸原图)", ct)
	}
	// 关键:降级响应绝不能被当作该衍生图 URL 的规范内容缓存,否则缓存投毒。
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("降级 Cache-Control = %q, 期望含 no-store(防缓存投毒)", cc)
	}
	if et := resp.Header.Get("ETag"); et != "" {
		t.Fatalf("降级不应带 ETag, 得到 %q", et)
	}
	if got := countFiles(t, vdir); got != 0 {
		t.Fatalf("处理失败后缓存文件数 = %d, 期望 0(未落缓存)", got)
	}
}
