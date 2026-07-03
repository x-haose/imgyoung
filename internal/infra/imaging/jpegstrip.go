// Package imaging 提供图片字节级处理(JPEG 元数据剥离)与 EXIF 档位定义。
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

// errMalformedJPEG 输入不是结构完整的 JPEG。
var errMalformedJPEG = errors.New("imaging: 畸形的 JPEG 结构")

// StripJPEGMetadata 移除 JPEG 中的全部元数据段与 EOI 后的尾部附加数据,
// 返回新的字节切片(不修改输入)。
// @Description: 纯段级字节操作,不解码、无损、零像素改动。
// 剥离范围见 isMetadataSegment(APPn 白名单 + COM);熵编码数据原样保留;
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

	i := 2
	for {
		if i+2 > len(data) {
			return nil, fmt.Errorf("%w: 段表在偏移 %d 处截断", errMalformedJPEG, i)
		}
		if data[i] != 0xFF {
			return nil, fmt.Errorf("%w: 偏移 %d 处不是段边界(0x%02X)", errMalformedJPEG, i, data[i])
		}
		// 标记前允许出现连续 0xFF 填充字节,跳过(不写回,等价于规范化)。
		if data[i+1] == 0xFF {
			i++
			continue
		}

		marker := data[i+1]
		switch {
		case marker == markerSOS:
			// 段表结束,其后为熵编码数据。熵编码流中 0xFF 均被填充为 FF00
			// (RST 为 FFD0–D7),裸 FFD9 必为真正的 EOI——在此截断,
			// 丢弃 EOI 之后的一切尾部附加数据(Motion Photo 视频、隐写载荷等)。
			tail := data[i:]
			if end := bytes.Index(tail, []byte{0xFF, markerEOI}); end >= 0 {
				tail = tail[:end+2]
			}
			out = append(out, tail...)
			return out, nil
		case marker == markerEOI:
			// 文件结束:只保留 EOI 本身,丢弃其后的尾部数据。
			out = append(out, data[i:i+2]...)
			return out, nil
		case marker == markerTEM, marker >= markerRST0 && marker <= markerRST7:
			// 无长度字段的独立标记,原样保留。
			out = append(out, data[i:i+2]...)
			i += 2
		default:
			if i+4 > len(data) {
				return nil, fmt.Errorf("%w: 段长度字段在偏移 %d 处截断", errMalformedJPEG, i)
			}
			segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
			if segLen < 2 || i+2+segLen > len(data) {
				return nil, fmt.Errorf("%w: 偏移 %d 处段长度越界", errMalformedJPEG, i)
			}
			if !isMetadataSegment(marker) {
				out = append(out, data[i:i+2+segLen]...)
			}
			i += 2 + segLen
		}
	}
}
