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
	markerAPP0  = 0xE0 // JFIF 头(结构必需,保留)
	markerAPP2  = 0xE2 // ICC 色彩配置(剥掉会改变颜色渲染,保留)
	markerAPP14 = 0xEE // Adobe 色彩转换标记(剥掉会导致解码偏色,保留)
	markerAPP15 = 0xEF // APPn 区间终点
	markerCOM   = 0xFE // 注释段(常含编辑软件信息,剥离)
	markerRST0  = 0xD0 // 重启标记区间起点(RST0–RST7 均无长度字段)
	markerRST7  = 0xD7 // 重启标记区间终点
)

// isMetadataSegment 判断段是否属于应剥离的元数据。
// @Description: 白名单策略——APPn 中仅保留 APP0/APP2/APP14(功能必需),
// 其余 APPn(Exif/XMP/IPTC/相机私有段等)与 COM 注释一律视为元数据剥离。
// @param marker 段标记(0xFF 后的第二字节)
// @return 是否应剥离
func isMetadataSegment(marker byte) bool {
	if marker == markerCOM {
		return true
	}
	if marker >= markerAPP0 && marker <= markerAPP15 {
		return marker != markerAPP0 && marker != markerAPP2 && marker != markerAPP14
	}
	return false
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

// truncateAtEOI 把从 SOS 起的字节截到首个 EOI 标记(含)为止,丢弃其后的尾部附加数据。
// @Description: 熵编码流中 0xFF 均被填充为 FF00(RST 为 FFD0–D7),裸 FFD9 必为真正的 EOI;
// 无 EOI 时原样返回。
// @param tail 从 SOS 标记起的字节
// @return 截断后的字节
func truncateAtEOI(tail []byte) []byte {
	if end := bytes.Index(tail, []byte{0xFF, markerEOI}); end >= 0 {
		return tail[:end+2]
	}
	return tail
}

// StripJPEGMetadata 移除 JPEG 中的全部元数据段与 EOI 后的尾部附加数据,
// 返回新的字节切片(不修改输入)。
// @Description: 纯段级字节操作,不解码、无损、零像素改动。
// 剥离范围见 isMetadataSegment(APPn 白名单 + COM);熵编码数据原样保留。
// 唯一例外是 EXIF 方向:像素不旋转,丢了它手机竖拍的照片会横着显示,故原图带非默认方向时
// 补回一个只含 Orientation 的最小 Exif 段,其余 EXIF 字段(GPS、机型、时间等)照删。
// 已知边界:渐进式 JPEG 多次扫描之间的段不扫描(极少携带元数据)。
// @param data 完整的 JPEG 字节
// @return 剥离后的 JPEG 字节
// @return 输入不是结构完整的 JPEG 时的错误
func StripJPEGMetadata(data []byte) ([]byte, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return nil, fmt.Errorf("%w: 缺少 SOI", errMalformedJPEG)
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:2]...)
	if o := jpegOrientation(data); o != orientationNormal {
		out = append(out, orientationSegment(o)...)
	}
	for i := 2; ; {
		seg, err := nextJPEGSegment(data, i)
		if err != nil {
			return nil, err
		}
		switch {
		case seg.marker == markerSOS:
			// 段表结束,其后为熵编码数据;EOI 之后的尾部附加数据(Motion Photo 视频、隐写载荷等)一并丢弃。
			return append(out, truncateAtEOI(data[seg.start:])...), nil
		case seg.marker == markerEOI:
			// 文件结束:只保留 EOI 本身,丢弃其后的尾部数据。
			return append(out, data[seg.start:seg.end]...), nil
		case isMetadataSegment(seg.marker):
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
