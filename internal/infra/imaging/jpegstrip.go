// Package imaging 提供图片字节级处理(元数据剥离、按需缩放重编码)与 EXIF 档位定义。
package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// JPEG 标记常量(第二字节;第一字节恒为 0xFF)。
const (
	markerSOI   = 0xD8 // 文件起始
	markerEOI   = 0xD9 // 文件结束
	markerSOS   = 0xDA // 扫描数据开始,其后为熵编码数据
	markerTEM   = 0x01 // 独立标记(无长度字段)
	markerAPP0  = 0xE0 // JFIF 头(内容为 JFIF 时保留)
	markerAPP2  = 0xE2 // ICC 色彩配置(内容为 ICC 时保留,剥掉会改变颜色渲染)
	markerAPP14 = 0xEE // Adobe 色彩转换标记(内容为 Adobe 时保留,剥掉会导致解码偏色)
	markerAPP15 = 0xEF // APPn 区间终点
	markerCOM   = 0xFE // 注释段(常含编辑软件信息,剥离)
	markerRST0  = 0xD0 // 重启标记区间起点(RST0–RST7 均无长度字段)
	markerRST7  = 0xD7 // 重启标记区间终点
)

// isMetadataSegment 判断段是否属于应剥离的元数据。
// @Description: 白名单策略——只保留功能必需的段:内容为 JFIF 的 APP0、ICC 配置的 APP2(分块的配置同样以
// ICC_PROFILE 开头)、Adobe 色彩转换标记的 APP14;其余 APPn(Exif/XMP/IPTC/相机私有段、JFXX 缩略图、
// MPF 等)与 COM 注释一律剥离。按内容而不只按标记号判断,是因为同一个 APPn 号也能承载任意数据。
// @param marker 段标记(0xFF 后的第二字节)
// @param payload 段负载(长度字段之后);无长度字段的标记为 nil
// @return 是否应剥离
func isMetadataSegment(marker byte, payload []byte) bool {
	switch marker {
	case markerCOM:
		return true
	case markerAPP0:
		return !bytes.HasPrefix(payload, []byte("JFIF\x00"))
	case markerAPP2:
		return !bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00"))
	case markerAPP14:
		return !bytes.HasPrefix(payload, []byte("Adobe"))
	default:
		return marker >= markerAPP0 && marker <= markerAPP15
	}
}

// segmentPayload 返回段负载(长度字段之后的字节)。
// @param data 完整的 JPEG 字节
// @param seg 段位置
// @return 负载;无长度字段的标记为 nil
func segmentPayload(data []byte, seg jpegSegment) []byte {
	if seg.end-seg.start < 4 {
		return nil
	}
	return data[seg.start+4 : seg.end]
}

// isStandaloneMarker 报告标记是否不带长度字段:TEM 与 RST0–RST7 只占两字节,其后紧接下一段。
// @param marker 段标记(0xFF 后的第二字节)
// @return 是否为独立标记
func isStandaloneMarker(marker byte) bool {
	return marker == markerTEM || (marker >= markerRST0 && marker <= markerRST7)
}

// errMalformedJPEG 输入不是结构完整的 JPEG。
var errMalformedJPEG = errors.New("imaging: 畸形的 JPEG 结构")

// jpegSegment 段表中一个条目在原始字节中的位置。
type jpegSegment struct {
	marker byte // 段标记(0xFF 后的第二字节)
	start  int  // 起始偏移,指向标记前的 0xFF
	end    int  // 结束偏移(不含);独立标记以及 SOS、EOI 为 start+2
}

// nextJPEGSegment 读取偏移 i 处的段表条目,跳过标记前的 0xFF 填充字节。
// @Description: SOS 之后是熵编码数据、EOI 之后是文件尾,两者都不再有段表意义上的长度,
// 与独立标记一样只占两字节返回,其后字节如何处理由调用方决定。
// @param data 完整的 JPEG 字节
// @param i 起始偏移,须指向一个 0xFF
// @return 段位置
// @return 段表在此处截断、不在段边界或长度越界时的错误
func nextJPEGSegment(data []byte, i int) (jpegSegment, error) {
	for i+2 <= len(data) && data[i] == 0xFF && data[i+1] == 0xFF {
		i++
	}
	if i+2 > len(data) {
		return jpegSegment{}, fmt.Errorf("%w: 段表在偏移 %d 处截断", errMalformedJPEG, i)
	}
	if data[i] != 0xFF {
		return jpegSegment{}, fmt.Errorf("%w: 偏移 %d 处不是段边界(0x%02X)", errMalformedJPEG, i, data[i])
	}
	marker := data[i+1]
	if marker == markerSOS || marker == markerEOI || isStandaloneMarker(marker) {
		return jpegSegment{marker: marker, start: i, end: i + 2}, nil
	}
	if i+4 > len(data) {
		return jpegSegment{}, fmt.Errorf("%w: 段长度字段在偏移 %d 处截断", errMalformedJPEG, i)
	}
	segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
	if segLen < 2 || i+2+segLen > len(data) {
		return jpegSegment{}, fmt.Errorf("%w: 偏移 %d 处段长度越界", errMalformedJPEG, i)
	}
	return jpegSegment{marker: marker, start: i, end: i + 2 + segLen}, nil
}

// scanEnd 返回从 SOS 起的一次扫描的结束偏移:扫描头之后的熵编码数据在下一个真正的标记处结束。
// @Description: 熵编码数据里 0xFF 一律填充为 FF00,重启标记为 FFD0–D7,连续的 0xFF 是填充,其余 FFxx 才是标记。
// @param data 完整的 JPEG 字节
// @param start SOS 标记的偏移
// @return 下一个标记的偏移;数据在标记之前结束时为 len(data)
// @return 扫描头长度越界时的错误
func scanEnd(data []byte, start int) (int, error) {
	if start+4 > len(data) {
		return 0, fmt.Errorf("%w: 扫描头在偏移 %d 处截断", errMalformedJPEG, start)
	}
	headerLen := int(binary.BigEndian.Uint16(data[start+2 : start+4]))
	p := start + 2 + headerLen
	if headerLen < 2 || p > len(data) {
		return 0, fmt.Errorf("%w: 偏移 %d 处扫描头长度越界", errMalformedJPEG, start)
	}
	for p+1 < len(data) {
		next := data[p+1]
		switch {
		case data[p] != 0xFF:
			p++
		case next == 0x00 || (next >= markerRST0 && next <= markerRST7):
			p += 2
		case next == 0xFF:
			p++
		default:
			return p, nil
		}
	}
	return len(data), nil
}

// copyScans 复制从 SOS 起的全部扫描,沿途丢弃扫描之间的元数据段,到 EOI 为止;EOI 之后的附加数据一并丢弃。
// @Description: 渐进式 JPEG 有多次扫描,元数据段可以出现在任意两次扫描之间;无 EOI 时复制到末尾。
// @param out 已写出的字节
// @param data 完整的 JPEG 字节
// @param i SOS 标记的偏移
// @return 追加后的字节
// @return 段表在扫描之间畸形时的错误
func copyScans(out, data []byte, i int) ([]byte, error) {
	for i < len(data) {
		seg, err := nextJPEGSegment(data, i)
		if err != nil {
			return nil, err
		}
		switch seg.marker {
		case markerEOI:
			return append(out, data[seg.start:seg.end]...), nil
		case markerSOS:
			end, err := scanEnd(data, seg.start)
			if err != nil {
				return nil, err
			}
			out = append(out, data[seg.start:end]...)
			i = end
		default:
			if !isMetadataSegment(seg.marker, segmentPayload(data, seg)) {
				out = append(out, data[seg.start:seg.end]...)
			}
			i = seg.end
		}
	}
	return out, nil
}

// StripJPEGMetadata 移除 JPEG 中的全部元数据段与 EOI 后的尾部附加数据,
// 返回新的字节切片(不修改输入)。
// @Description: 纯段级字节操作,不解码、无损、零像素改动。
// 剥离范围见 isMetadataSegment(APPn 白名单 + COM),含渐进式 JPEG 各次扫描之间的段;熵编码数据原样保留。
// 唯一例外是 EXIF 方向:像素不旋转,丢了它手机竖拍的照片会横着显示,故原图带非默认方向时
// 补回一个只含 Orientation 的最小 Exif 段,其余 EXIF 字段(GPS、机型、时间等)照删。
// @param data 完整的 JPEG 字节
// @return 剥离后的 JPEG 字节
// @return 输入不是结构完整的 JPEG 时的错误
func StripJPEGMetadata(data []byte) ([]byte, error) {
	return stripJPEG(data, true)
}

// stripJPEG 是 StripJPEGMetadata 的实现,keepOrientation 为 false 时连方向标记也不保留。
// @param data 完整的 JPEG 字节
// @param keepOrientation 原图带非默认方向时是否补回只含方向的 EXIF 段
// @return 剥离后的 JPEG 字节
// @return 输入不是结构完整的 JPEG 时的错误
func stripJPEG(data []byte, keepOrientation bool) ([]byte, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return nil, fmt.Errorf("%w: 缺少 SOI", errMalformedJPEG)
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:2]...)
	if o := jpegOrientation(data); keepOrientation && o != orientationNormal {
		out = append(out, orientationSegment(o)...)
	}
	for i := 2; ; {
		seg, err := nextJPEGSegment(data, i)
		if err != nil {
			return nil, err
		}
		switch {
		case seg.marker == markerSOS:
			// 其后为熵编码数据与可能的后续扫描;EOI 之后的尾部附加数据(Motion Photo 视频、隐写载荷等)一并丢弃。
			return copyScans(out, data, seg.start)
		case seg.marker == markerEOI:
			// 文件结束:只保留 EOI 本身,丢弃其后的尾部数据。
			return append(out, data[seg.start:seg.end]...), nil
		case isMetadataSegment(seg.marker, segmentPayload(data, seg)):
			// 元数据段:丢弃。
		default:
			out = append(out, data[seg.start:seg.end]...)
		}
		i = seg.end
	}
}

// orientationSegment 构造只含 IFD0 Orientation 一条记录的最小 APP1/Exif 段(大端 TIFF)。
// @Description: 段长 36 字节:标记(2)+ 长度(2)+ "Exif\0\0"(6)+ TIFF 头(8)+ 记录数(2)
// + 一条记录(12)+ 下一 IFD 偏移(4)。紧跟 SOI 放置,解码器与浏览器都会读到。
// @param o EXIF 方向值[2,8]
// @return 完整的段字节
func orientationSegment(o int) []byte {
	seg := make([]byte, 0, 36)
	seg = append(seg, 0xFF, markerAPP1, 0, 34)
	seg = append(seg, exifPrefix...)
	seg = append(seg, 'M', 'M', 0, 0x2A, 0, 0, 0, tiffHeaderLen)
	seg = append(seg, 0, 1)
	seg = binary.BigEndian.AppendUint16(seg, exifOrientationTag)
	seg = append(seg, 0, 3, 0, 0, 0, 1)        // 类型 SHORT,个数 1
	seg = append(seg, 0, orientationValues[o]) // 值(大端 SHORT)
	return append(seg, 0, 0, 0, 0, 0, 0)       // 值字段余下 2 字节 + 下一 IFD 偏移 0
}

// orientationValues 方向值到字节的对照;方向值来自 readOrientation,恒在 [1,8],越界即程序错误。
var orientationValues = [9]byte{0, 1, 2, 3, 4, 5, 6, 7, 8}
