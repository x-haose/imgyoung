//go:build e2e

// 按需处理管道(迭代 6)的 e2e 覆盖:缩放/转格式/webp 协商/Vary/ETag-304/
// EXIF orientation 校正/动图保护/档外参数——现有 e2e_test.go 早于迭代 6,未覆盖这些。
package e2e

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"io"
	"net/http"
	"testing"
)

// procSettings 钉死处理档位与格式,使处理测试不依赖出厂默认配置。
var procSettings = map[string]any{
	"process_widths":  []int{320, 640, 1280},
	"process_formats": []string{"webp", "jpeg", "png"},
}

// TestProcessResize 缩放:jpeg 上传后 ?w=320 应返回宽 320 的图。
func TestProcessResize(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(800, 600, 0), nil, nil)
	_, body := procGet(t, inst, url+"?w=320", nil)
	if info := procInspect(body); info.width != 320 {
		t.Errorf("?w=320 期望宽 320,得 %dx%d", info.width, info.height)
	}
}

// TestProcessFormatConvert 转格式:?format=webp 应返回 webp。
func TestProcessFormatConvert(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(640, 480, 0), nil, nil)
	resp, body := procGet(t, inst, url+"?format=webp", nil)
	if ct := resp.Header.Get("Content-Type"); ct != "image/webp" {
		t.Errorf("Content-Type 期望 image/webp,得 %q", ct)
	}
	if procSniff(body) != "webp" {
		t.Errorf("magic 期望 webp,得 %s", procSniff(body))
	}
}

// TestProcessNegotiationVary Accept 协商 + Vary:带 Accept image/webp 且无 format 应协商为 webp,响应带 Vary: Accept。
func TestProcessNegotiationVary(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(640, 480, 0), nil, nil)
	resp, body := procGet(t, inst, url+"?w=320", map[string]string{"Accept": "image/webp"})
	if procSniff(body) != "webp" {
		t.Errorf("Accept 协商期望 webp,得 %s", procSniff(body))
	}
	if vary := resp.Header.Get("Vary"); vary != "Accept" {
		t.Errorf("协商响应期望 Vary: Accept,得 %q", vary)
	}
}

// TestProcessETag304 变体 ETag/304:带处理参数取回得 ETag,再带 If-None-Match 应 304。
func TestProcessETag304(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(640, 480, 0), nil, nil)
	resp, _ := procGet(t, inst, url+"?w=320", nil)
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("变体响应缺 ETag")
	}
	resp2, _ := procGet(t, inst, url+"?w=320", map[string]string{"If-None-Match": etag})
	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("If-None-Match 命中期望 304,得 %d", resp2.StatusCode)
	}
}

// TestProcessOrientation EXIF orientation 校正(L5):800×600 左红右蓝 + orient=6 →
// ?w=320 应得 320×426 竖图,且顶部像素偏红(顺时针 90 后原左半红转到顶部)。
func TestProcessOrientation(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(800, 600, 6), nil, nil)
	_, body := procGet(t, inst, url+"?w=320", nil)
	info := procInspect(body)
	if info.width != 320 || info.height != 426 {
		t.Fatalf("orientation 未校正:期望 320×426,得 %dx%d", info.width, info.height)
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("解码变体失败: %v", err)
	}
	r, _, b, _ := img.At(img.Bounds().Dx()/2, 3).RGBA()
	if r <= b {
		t.Errorf("orientation 像素落位错:顶部应偏红,得 r=%d b=%d", r>>8, b>>8)
	}
}

// TestProcessGifAnimationProtected 动图保护:gif 带处理参数仍应回原动图(不可重编码、不拍成静图)。
func TestProcessGifAnimationProtected(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	src := procGIF(100, 100, 4)
	_, url := mustUpload(t, inst, src, nil, nil)
	// 原样取回应仍是 gif 且字节一致。
	_, body := procGet(t, inst, url, nil)
	if procSniff(body) != "gif" || !bytes.Equal(body, src) {
		t.Errorf("gif 原样取回失败:fmt=%s equal=%v", procSniff(body), bytes.Equal(body, src))
	}
	// 带处理参数仍应回原 gif(动图保护)。
	_, body2 := procGet(t, inst, url+"?w=320&format=webp", nil)
	if procSniff(body2) != "gif" {
		t.Errorf("gif 带处理参数应回原动图,得 %s(动图保护失效)", procSniff(body2))
	}
}

// TestProcessBadParams 档外参数应 400。
func TestProcessBadParams(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(640, 480, 0), nil, nil)
	if resp, _ := procGet(t, inst, url+"?w=999", nil); resp.StatusCode != 400 {
		t.Errorf("档外宽度期望 400,得 %d", resp.StatusCode)
	}
	if resp, _ := procGet(t, inst, url+"?format=tiff", nil); resp.StatusCode != 400 {
		t.Errorf("档外格式期望 400,得 %d", resp.StatusCode)
	}
}

// procGet 带可选请求头的 GET,返回响应与正文;不自动跟随重定向。
func procGet(t *testing.T, inst *instance, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, inst.baseURL+path, nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发起请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

// ---- 处理管道专用图片工具(独立命名,避免与现有 helper 冲突)----

type procImg struct {
	format        string
	width, height int
}

func procInspect(data []byte) procImg {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return procImg{format: procSniff(data)}
	}
	return procImg{format: format, width: cfg.Width, height: cfg.Height}
}

func procSniff(d []byte) string {
	switch {
	case len(d) >= 12 && string(d[0:4]) == "RIFF" && string(d[8:12]) == "WEBP":
		return "webp"
	case len(d) >= 3 && d[0] == 0xFF && d[1] == 0xD8:
		return "jpeg"
	case len(d) >= 8 && string(d[1:4]) == "PNG":
		return "png"
	case len(d) >= 6 && string(d[0:3]) == "GIF":
		return "gif"
	}
	return "unknown"
}

// procJPEG 生成左红右蓝 JPEG,可选注入真实 EXIF orientation。
func procJPEG(w, h, orient int) []byte {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, procTwoTone(w, h), &jpeg.Options{Quality: 92})
	data := buf.Bytes()
	if orient <= 0 {
		return data
	}
	le := binary.LittleEndian
	ifd := make([]byte, 2+12+4)
	le.PutUint16(ifd[0:], 1)
	le.PutUint16(ifd[2:], 0x0112)
	le.PutUint16(ifd[4:], 3)
	le.PutUint32(ifd[6:], 1)
	le.PutUint16(ifd[10:], uint16(orient)) //nolint:gosec // orient 恒为 1–8
	tiff := make([]byte, 8)
	copy(tiff, "II")
	le.PutUint16(tiff[2:], 0x002A)
	le.PutUint32(tiff[4:], 8)
	tiff = append(tiff, ifd...)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2)) //nolint:gosec // payload 为几十字节测试常量
	seg = append(seg, payload...)
	out := append([]byte{}, data[:2]...)
	out = append(out, seg...)
	return append(out, data[2:]...)
}

func procGIF(w, h, frames int) []byte {
	g := &gif.GIF{}
	pal := color.Palette{color.RGBA{R: 220, A: 255}, color.RGBA{B: 220, A: 255}}
	for f := 0; f < frames; f++ {
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		idx := uint8(f % 2)
		for i := range img.Pix {
			img.Pix[i] = idx
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	_ = gif.EncodeAll(&buf, g)
	return buf.Bytes()
}

func procTwoTone(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				m.Set(x, y, color.RGBA{R: 220, A: 255})
			} else {
				m.Set(x, y, color.RGBA{B: 220, A: 255})
			}
		}
	}
	return m
}
