package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

// 目标格式白名单;取值兼作 variants 缓存键的一部分,白名单外一律拒绝。
const (
	FormatWebP = "webp"
	FormatJPEG = "jpeg"
	FormatPNG  = "png"
)

var (
	// ErrProcTooManyPixels 源图解码后像素数超预算,拒绝处理以护内存(解码后 RGBA 内存由分辨率而非字节数决定)。
	ErrProcTooManyPixels = errors.New("图片像素数超预算")
	// ErrBadFormat 目标格式不在白名单。
	ErrBadFormat = errors.New("不支持的目标格式")
	// ErrProcInvalidImage 源字节非可解码图片。
	ErrProcInvalidImage = errors.New("无效图片")
)

// EncodeSettings 编码档位配置,由运行配置注入 Processor;质量固定档既压 CPU 又提高缓存命中率。
type EncodeSettings struct {
	QualityWebP int   // libwebp 质量[0,100]
	QualityJPEG int   // stdlib jpeg 质量[1,100]
	WebPMethod  int   // libwebp 质量/速度权衡[0,6]
	MaxPixels   int64 // 源图像素预算(w×h 上限)
}

// Processor 按需图像处理引擎,纯 Go 实现。
// @Description: 图像处理引擎的替换点(两条升级缝之一):接入 libvips 等引擎时在此抽出接口,调用方不变。
type Processor struct {
	set EncodeSettings // 编码档位,构造时注入、运行期只读
}

// NewProcessor 用给定编码档位构造处理器。
// @param set 编码档位配置
// @return 处理器
func NewProcessor(set EncodeSettings) *Processor {
	return &Processor{set: set}
}

// ProcessParams 按需处理参数;档位合法性由调用方白名单保证,本引擎只护像素预算、不复核档位。
type ProcessParams struct {
	Width  int    // 目标宽度像素;0 或 ≥ 源宽表示不缩放(只缩小,绝不放大)
	Format string // 目标格式,取 Format* 常量之一
}

// Process 将源图字节按参数等比缩放并重编码为目标格式。
// @Description: 先读图头做像素预算闸,再 decode→按需缩小(保持宽高比,CatmullRom)→按配置质量档编码。全程纯 Go,CGO-free。
// @param src 源图完整字节(任意受支持格式)
// @param p 处理参数(宽度档 + 目标格式)
// @return 编码后字节
// @return 目标 MIME
// @return 错误
func (proc *Processor) Process(src []byte, p ProcessParams) ([]byte, string, error) {
	// 只读图片头取尺寸,超预算直接拒绝、不进完整解码器。
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrProcInvalidImage, err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > proc.set.MaxPixels {
		return nil, "", ErrProcTooManyPixels
	}

	img, format, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrProcInvalidImage, err)
	}
	// JPEG 按 EXIF 方向校正到视觉正确姿态,使目标宽度作用于呈现方向;标准库解码器不自动处理方向。
	if format == "jpeg" {
		img = applyOrientation(img, jpegOrientation(src))
	}
	// 缩放判据用校正后的实际宽(方向 5–8 会交换宽高),而非解码头的原始宽。
	srcWidth := img.Bounds().Dx()
	if p.Width > 0 && p.Width < srcWidth {
		img = resizeToWidth(img, p.Width) // 缩放输出即 RGBA
	} else {
		img = toRGBA(img) // 不缩放时也归一化,规避编码器对非连续布局的损坏
	}

	var buf bytes.Buffer
	switch p.Format {
	case FormatWebP:
		err = webp.Encode(&buf, img, webp.Options{Quality: proc.set.QualityWebP, Method: proc.set.WebPMethod})
	case FormatJPEG:
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: proc.set.QualityJPEG})
	case FormatPNG:
		// PNG 无损、无质量档;固定最高压缩,适合图形与需透明的场景。
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
	default:
		return nil, "", ErrBadFormat
	}
	if err != nil {
		return nil, "", fmt.Errorf("编码 %s: %w", p.Format, err)
	}
	return buf.Bytes(), mimeForFormat(p.Format), nil
}

// resizeToWidth 等比缩放到目标宽度(仅缩小),用 CatmullRom 保留清晰度。
// @Description: 高度按宽高比整数换算,可能差一像素,视觉无感;结果为 RGBA。
// @param src 源图像
// @param width 目标宽度像素
// @return 缩放后图像
func resizeToWidth(src image.Image, width int) image.Image {
	b := src.Bounds()
	height := b.Dy() * width / b.Dx()
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

// toRGBA 将任意图像归一化为 *image.RGBA;已是 RGBA 则原样返回。
// @Description: 编码器对非连续 NYCbCrA 布局的图像会产出损坏字节,统一转 RGBA 从源头规避,
// 并让编码路径只面对单一像素布局。
// @param src 源图像
// @return RGBA 图像
func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// ValidateFormat 校验目标格式是否在支持白名单内。
// @param format 目标格式
// @return 不支持时返回 ErrBadFormat
func ValidateFormat(format string) error {
	if mimeForFormat(format) == "" {
		return fmt.Errorf("%w: %q", ErrBadFormat, format)
	}
	return nil
}

// MIME 返回目标格式对应的 MIME;白名单外返回空串,供命中缓存时定 Content-Type。
// @param format 目标格式
// @return MIME 字符串
func MIME(format string) string { return mimeForFormat(format) }

// mimeForFormat 返回目标格式对应的 MIME;白名单外返回空串。
// @param format 目标格式,取 Format* 常量
// @return MIME 字符串
func mimeForFormat(format string) string {
	switch format {
	case FormatWebP:
		return "image/webp"
	case FormatJPEG:
		return "image/jpeg"
	case FormatPNG:
		return "image/png"
	default:
		return ""
	}
}
