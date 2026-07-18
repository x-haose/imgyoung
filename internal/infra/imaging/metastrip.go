package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// StripMetadata 按 MIME 分发到对应格式的元数据剥离器,返回新的字节切片。
// @Description: JPEG/PNG/WebP 做段/chunk 级剥离(不解码、零像素改动);GIF 视为
// 无标准元数据容器(规范仅注释扩展,不承载 EXIF/XMP)直通;其余未知 MIME 原样返回。
// @param data 完整图片字节
// @param mime 真实 MIME(调用方经 magic bytes 判定)
// @return 剥离后的字节(直通格式即原切片)
// @return 输入结构畸形时的错误
func StripMetadata(data []byte, mime string) ([]byte, error) {
	switch mime {
	case "image/jpeg":
		return StripJPEGMetadata(data)
	case "image/png":
		return StripPNGMetadata(data)
	case "image/webp":
		return StripWebPMetadata(data)
	default:
		return data, nil
	}
}

// errMalformedPNG 输入不是结构完整的 PNG。
var errMalformedPNG = errors.New("imaging: 畸形的 PNG 结构")

// pngSignature PNG 文件固定签名(8 字节)。
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}

// pngChunkOverhead 单个 PNG chunk 的定长开销:长度(4)+ 类型(4)+ CRC(4)。
const pngChunkOverhead = 12

// pngMetadataChunks 应剥离的 PNG 元数据 chunk 类型(EXIF 与三种文本块;
// 其余辅助块如 gAMA/iCCP 影响颜色渲染,保留)。
var pngMetadataChunks = map[string]struct{}{
	"eXIf": {},
	"tEXt": {},
	"zTXt": {},
	"iTXt": {},
}

// StripPNGMetadata 移除 PNG 中的元数据 chunk 与 IEND 后的尾部附加数据,
// 返回新的字节切片(不修改输入)。
// @Description: 纯 chunk 级字节操作:保留的 chunk 原样拷贝(CRC 不动),
// 剥离范围见 pngMetadataChunks;IEND 之后的一切尾部数据一并丢弃。
// @param data 完整的 PNG 字节
// @return 剥离后的 PNG 字节
// @return 输入不是结构完整的 PNG 时的错误
func StripPNGMetadata(data []byte) ([]byte, error) {
	if len(data) < len(pngSignature) || !bytes.Equal(data[:len(pngSignature)], pngSignature) {
		return nil, fmt.Errorf("%w: 缺少 PNG 签名", errMalformedPNG)
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:len(pngSignature)]...)
	for i := len(pngSignature); ; {
		if i+pngChunkOverhead > len(data) {
			return nil, fmt.Errorf("%w: chunk 表在偏移 %d 处截断", errMalformedPNG, i)
		}
		length := int(binary.BigEndian.Uint32(data[i : i+4]))
		end := i + pngChunkOverhead + length
		if end < i || end > len(data) {
			return nil, fmt.Errorf("%w: 偏移 %d 处 chunk 长度越界", errMalformedPNG, i)
		}
		ctype := string(data[i+4 : i+8])
		if _, drop := pngMetadataChunks[ctype]; !drop {
			out = append(out, data[i:end]...)
		}
		if ctype == "IEND" {
			// 文件结束:丢弃其后的一切尾部附加数据(隐写载荷等)。
			return out, nil
		}
		i = end
	}
}

// errMalformedWebP 输入不是结构完整的 WebP。
var errMalformedWebP = errors.New("imaging: 畸形的 WebP 结构")

// RIFF/WebP 容器常量。
const (
	webpHeaderSize  = 12   // "RIFF" + 总长(4)+ "WEBP"
	riffChunkHeader = 8    // chunk FourCC(4)+ 长度(4)
	vp8xFlagEXIF    = 0x08 // VP8X 标志字节的 EXIF 存在位
	vp8xFlagXMP     = 0x04 // VP8X 标志字节的 XMP 存在位
	webpChunkEXIF   = "EXIF"
	webpChunkXMP    = "XMP " // FourCC 定长 4 字节,XMP 以空格补齐
	webpChunkVP8X   = "VP8X"
)

// StripWebPMetadata 移除 WebP RIFF 容器中的 EXIF/XMP chunk,同步清零 VP8X 头的
// 对应标志位并修正 RIFF 总长度字段,返回新的字节切片(不修改输入)。
// @Description: 纯 chunk 级字节操作,不解码;像素 chunk(VP8/VP8L/ANMF 等)原样保留。
// RIFF 声明长度之外的尾部字节一并丢弃。
// @param data 完整的 WebP 字节
// @return 剥离后的 WebP 字节
// @return 输入不是结构完整的 WebP 时的错误
func StripWebPMetadata(data []byte) ([]byte, error) {
	if len(data) < webpHeaderSize || string(data[:4]) != "RIFF" || string(data[8:webpHeaderSize]) != "WEBP" {
		return nil, fmt.Errorf("%w: 缺少 RIFF/WEBP 头", errMalformedWebP)
	}
	riffEnd := riffChunkHeader + int(binary.LittleEndian.Uint32(data[4:riffChunkHeader]))
	if riffEnd < webpHeaderSize || riffEnd > len(data) {
		return nil, fmt.Errorf("%w: RIFF 总长度越界", errMalformedWebP)
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:webpHeaderSize]...)
	vp8xFlagsAt := -1 // out 中 VP8X 标志字节的下标;-1 表示尚未见到 VP8X
	for i := webpHeaderSize; i < riffEnd; {
		if i+riffChunkHeader > riffEnd {
			return nil, fmt.Errorf("%w: chunk 表在偏移 %d 处截断", errMalformedWebP, i)
		}
		fourCC := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+riffChunkHeader]))
		// chunk 数据按偶数对齐,奇数长度补一填充字节;末 chunk 允许缺失该填充。
		padded := size + size%2
		end := i + riffChunkHeader + padded
		if end < i || end > riffEnd {
			end = i + riffChunkHeader + size
			if end < i || end > riffEnd {
				return nil, fmt.Errorf("%w: 偏移 %d 处 chunk 长度越界", errMalformedWebP, i)
			}
		}
		switch fourCC {
		case webpChunkEXIF, webpChunkXMP:
			// 元数据 chunk:丢弃。
		case webpChunkVP8X:
			if size < 1 {
				return nil, fmt.Errorf("%w: VP8X 头为空", errMalformedWebP)
			}
			// 只认首个 VP8X(规范容器仅一个;畸形多头时只清首个,其余原样保留)。
			if vp8xFlagsAt < 0 {
				vp8xFlagsAt = len(out) + riffChunkHeader
			}
			out = append(out, data[i:end]...)
		default:
			out = append(out, data[i:end]...)
		}
		i = end
	}
	if vp8xFlagsAt >= 0 {
		out[vp8xFlagsAt] &^= vp8xFlagEXIF | vp8xFlagXMP
	}
	binary.LittleEndian.PutUint32(out[4:riffChunkHeader], uint32(len(out)-riffChunkHeader)) //nolint:gosec // 输出不长于输入,RIFF 容器本身以 uint32 记长,合法输入不溢出
	return out, nil
}
