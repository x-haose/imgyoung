package imaging

import (
	"encoding/binary"
	"image"
)

// JPEG APP1 段标记(EXIF 承载段;jpegstrip 的白名单剥离刻意保留 APP0/2/14,APP1 属被剥离范围)。
const markerAPP1 = 0xE1

// EXIF/TIFF 解析常量。
const (
	exifOrientationTag = 0x0112 // IFD0 中的方向 tag
	orientationNormal  = 1      // 无需变换的默认方向
	tiffHeaderLen      = 8      // 字节序(2)+ 魔数 0x002A(2)+ IFD0 偏移(4)
	ifdEntryLen        = 12     // 单条 IFD entry:tag(2)+type(2)+count(4)+value/offset(4)
)

// jpegOrientation 从 JPEG 字节读取 EXIF Orientation(1–8)。
// @Description: 只读 APP1/Exif 段的 IFD0 Orientation tag,不解码像素;容错优先——
// 无 EXIF、非 JPEG 或任何解析异常都退回"正常方向"而非报错。
// @param src 源图字节
// @return EXIF 方向值[1,8]
func jpegOrientation(src []byte) int {
	tiff := findEXIFTIFF(src)
	if tiff == nil {
		return orientationNormal
	}
	return readOrientation(tiff)
}

// findEXIFTIFF 遍历 JPEG 段表,返回 APP1/Exif 段内 TIFF 数据的起始切片;未找到返回 nil。
// @param data JPEG 字节
// @return "Exif\0\0" 之后的 TIFF 字节,或 nil
func findEXIFTIFF(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return nil
	}
	const exifPrefix = "Exif\x00\x00"
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return nil
		}
		marker := data[i+1]
		if marker == 0xFF { // 标记前填充字节
			i++
			continue
		}
		if marker == markerSOS || marker == markerEOI { // 其后无元数据段
			return nil
		}
		if marker == markerTEM || (marker >= markerRST0 && marker <= markerRST7) { // 无长度字段
			i += 2
			continue
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(data) {
			return nil
		}
		if marker == markerAPP1 {
			seg := data[i+4 : i+2+segLen]
			if len(seg) >= len(exifPrefix) && string(seg[:len(exifPrefix)]) == exifPrefix {
				return seg[len(exifPrefix):]
			}
		}
		i += 2 + segLen
	}
	return nil
}

// readOrientation 从 TIFF 头解析 IFD0 的 Orientation tag;任何越界或异常退回正常方向。
// @param tiff "Exif\0\0" 之后的 TIFF 字节
// @return 方向值[1,8]
func readOrientation(tiff []byte) int {
	if len(tiff) < tiffHeaderLen {
		return orientationNormal
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return orientationNormal
	}
	if bo.Uint16(tiff[2:4]) != 0x002A {
		return orientationNormal
	}
	ifd := int(bo.Uint32(tiff[4:8]))
	if ifd < tiffHeaderLen || ifd+2 > len(tiff) {
		return orientationNormal
	}
	count := int(bo.Uint16(tiff[ifd : ifd+2]))
	entry := ifd + 2
	for n := 0; n < count; n++ {
		if entry+ifdEntryLen > len(tiff) {
			break
		}
		if bo.Uint16(tiff[entry:entry+2]) == exifOrientationTag {
			// SHORT 类型,值内联在 entry+8 处(前 2 字节)。
			v := int(bo.Uint16(tiff[entry+8 : entry+10]))
			if v >= 1 && v <= 8 {
				return v
			}
			return orientationNormal
		}
		entry += ifdEntryLen
	}
	return orientationNormal
}

// applyOrientation 按 EXIF 方向把图像旋转/翻转到视觉正确姿态;正常方向原样返回。
// @Description: 处理链路在缩放前调用,使目标宽度作用于视觉呈现方向;方向 5–8 交换宽高。
// 逐像素映射,输入受像素预算闸约束,开销可控。
// @param src 源图像
// @param o EXIF 方向值[1,8]
// @return 姿态校正后的 RGBA 图像;o 为正常方向时返回原图
func applyOrientation(src image.Image, o int) image.Image {
	if o <= orientationNormal || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 { // 5–8 为含转置的方向,输出宽高互换
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			sx, sy := srcCoord(o, dx, dy, w, h)
			dst.Set(dx, dy, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

// srcCoord 给定目标坐标,返回 EXIF 方向 o 下对应的源坐标(源尺寸 w×h)。
// @param o 方向值[2,8]
// @param dx 目标横坐标
// @param dy 目标纵坐标
// @param w 源宽
// @param h 源高
// @return 源横坐标
// @return 源纵坐标
func srcCoord(o, dx, dy, w, h int) (int, int) {
	switch o {
	case 2: // 水平镜像
		return w - 1 - dx, dy
	case 3: // 旋转 180
		return w - 1 - dx, h - 1 - dy
	case 4: // 垂直镜像
		return dx, h - 1 - dy
	case 5: // 沿主对角线转置
		return dy, dx
	case 6: // 顺时针 90
		return dy, h - 1 - dx
	case 7: // 沿副对角线转置
		return w - 1 - dy, h - 1 - dx
	case 8: // 顺时针 270
		return w - 1 - dy, dx
	default:
		return dx, dy
	}
}
