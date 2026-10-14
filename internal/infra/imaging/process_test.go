package imaging

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	_ "golang.org/x/image/webp" // 注册 webp 解码器,供断言输出尺寸
)

// testProc 构造带默认档位、指定像素预算的处理器。
func testProc(maxPixels int64) *Processor {
	return NewProcessor(EncodeSettings{
		QualityWebP: 80, QualityJPEG: 85, WebPMethod: 4, MaxPixels: maxPixels,
	})
}

// srcPNG 合成一张 w×h 的 PNG 源图字节,用作处理输入。
func srcPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatalf("合成源图: %v", err)
	}
	return buf.Bytes()
}

func TestProcessEncodesEachFormat(t *testing.T) {
	src := srcPNG(t, 200, 150)
	const budget = 1 << 30
	proc := testProc(budget)
	for _, format := range []string{FormatWebP, FormatJPEG, FormatPNG} {
		out, mime, err := proc.Process(src, ProcessParams{Format: format})
		if err != nil {
			t.Fatalf("Process(%s): %v", format, err)
		}
		if len(out) == 0 {
			t.Fatalf("Process(%s): 输出为空", format)
		}
		if mime != mimeForFormat(format) {
			t.Fatalf("Process(%s): mime=%q 期望 %q", format, mime, mimeForFormat(format))
		}
	}
}

func TestProcessResizeOnlyShrinks(t *testing.T) {
	src := srcPNG(t, 200, 150)
	const budget = 1 << 30
	proc := testProc(budget)

	// 缩小到宽 100:等比,高应为 75。
	out, _, err := proc.Process(src, ProcessParams{Width: 100, Format: FormatWebP})
	if err != nil {
		t.Fatalf("Process 缩小: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("解码输出: %v", err)
	}
	if cfg.Width != 100 || cfg.Height != 75 {
		t.Fatalf("缩小后尺寸=%dx%d 期望 100x75", cfg.Width, cfg.Height)
	}

	// 请求宽度 ≥ 源宽:不放大,保持 200。
	out, _, err = proc.Process(src, ProcessParams{Width: 400, Format: FormatWebP})
	if err != nil {
		t.Fatalf("Process 不放大: %v", err)
	}
	cfg, _, err = image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("解码输出: %v", err)
	}
	if cfg.Width != 200 {
		t.Fatalf("不放大宽度=%d 期望 200", cfg.Width)
	}
}

func TestProcessRejects(t *testing.T) {
	src := srcPNG(t, 200, 150)

	// 像素预算:200×150=30000,预算设 29999 应拒绝。
	_, _, err := testProc(29999).Process(src, ProcessParams{Format: FormatWebP})
	if !errors.Is(err, ErrProcTooManyPixels) {
		t.Fatalf("超预算未拒绝: %v", err)
	}
	proc := testProc(1 << 30)
	// 目标格式白名单外。
	if _, _, err := proc.Process(src, ProcessParams{Format: "gif"}); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("坏格式未拒绝: %v", err)
	}
	// 非图片字节。
	_, _, err = proc.Process([]byte("not an image"), ProcessParams{Format: FormatWebP})
	if !errors.Is(err, ErrProcInvalidImage) {
		t.Fatalf("坏图片未拒绝: %v", err)
	}
}
