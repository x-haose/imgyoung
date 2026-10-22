package imaging

import (
	"encoding/binary"
	"slices"
)

// TIFF 字段类型编号及其单个元素的字节数。
const (
	tiffByte      = 1  // 无符号字节,1 字节
	tiffASCII     = 2  // 以 NUL 结尾的字符串,每字符 1 字节
	tiffShort     = 3  // 无符号 16 位整数,2 字节
	tiffLong      = 4  // 无符号 32 位整数,4 字节
	tiffRational  = 5  // 两个无符号 32 位整数(分子、分母),8 字节
	tiffUndefined = 7  // 按字节解释的任意数据,1 字节
	tiffSRational = 10 // 两个有符号 32 位整数(分子、分母),8 字节
)

// 指向子 IFD 的 tag。
const (
	tagExifIFD = 0x8769 // IFD0 中指向 Exif 子 IFD 的偏移
	tagGPSIFD  = 0x8825 // IFD0 中指向 GPS 子 IFD 的偏移
)

// tiffEntry IFD 中的一条记录,值已按大端编码。
type tiffEntry struct {
	tag   uint16 // 字段编号
	typ   uint16 // 字段类型,取 tiff* 常量
	count uint32 // 元素个数
	value []byte // 编码后的值,长度为 count × 元素字节数
}

// asciiEntry 构造字符串记录,值末尾补 NUL。
// @param tag 字段编号
// @param s 字符串
// @return 记录
func asciiEntry(tag uint16, s string) tiffEntry {
	b := append([]byte(s), 0)
	return tiffEntry{tag: tag, typ: tiffASCII, count: uint32(len(b)), value: b}
}

// shortEntry 构造单个 16 位整数记录。
// @param tag 字段编号
// @param v 取值
// @return 记录
func shortEntry(tag, v uint16) tiffEntry {
	return tiffEntry{tag: tag, typ: tiffShort, count: 1, value: binary.BigEndian.AppendUint16(nil, v)}
}

// longEntry 构造单个 32 位整数记录。
// @param tag 字段编号
// @param v 取值
// @return 记录
func longEntry(tag uint16, v uint32) tiffEntry {
	return tiffEntry{tag: tag, typ: tiffLong, count: 1, value: binary.BigEndian.AppendUint32(nil, v)}
}

// rationalEntry 构造一组无符号有理数记录。
// @param tag 字段编号
// @param pairs 若干 {分子, 分母}
// @return 记录
func rationalEntry(tag uint16, pairs ...[2]uint32) tiffEntry {
	var b []byte
	for _, p := range pairs {
		b = binary.BigEndian.AppendUint32(b, p[0])
		b = binary.BigEndian.AppendUint32(b, p[1])
	}
	return tiffEntry{tag: tag, typ: tiffRational, count: uint32(len(pairs)), value: b}
}

// srationalEntry 构造一个有符号有理数记录。
// @param tag 字段编号
// @param num 分子
// @param den 分母
// @return 记录
func srationalEntry(tag uint16, num, den int32) tiffEntry {
	b := binary.BigEndian.AppendUint32(nil, uint32(num))
	b = binary.BigEndian.AppendUint32(b, uint32(den))
	return tiffEntry{tag: tag, typ: tiffSRational, count: 1, value: b}
}

// byteEntry 构造字节数组记录。
// @param tag 字段编号
// @param b 字节
// @return 记录
func byteEntry(tag uint16, b ...byte) tiffEntry {
	return tiffEntry{tag: tag, typ: tiffByte, count: uint32(len(b)), value: b}
}

// undefinedEntry 构造按字节解释的记录。
// @param tag 字段编号
// @param b 字节
// @return 记录
func undefinedEntry(tag uint16, b []byte) tiffEntry {
	return tiffEntry{tag: tag, typ: tiffUndefined, count: uint32(len(b)), value: b}
}

// ifdSize 返回含 n 条记录的 IFD 本身占用的字节数:记录数(2)+ 记录(12 × n)+ 下一 IFD 偏移(4)。
// @param n 记录条数
// @return 字节数
func ifdSize(n int) int { return 2 + ifdEntryLen*n + 4 }

// buildTIFF 把 IFD0、Exif 子 IFD 与 GPS 子 IFD 编码成大端 TIFF 字节。
// @Description: 布局依次为 TIFF 头、IFD0、Exif IFD、GPS IFD、超过 4 字节的值。IFD0 的两条子 IFD 指针由本函数补入,
// 调用方不必给;各 IFD 内的记录按 tag 升序写出,这是 TIFF 规范对读取方的前提。
// @param ifd0 IFD0 的记录
// @param exif Exif 子 IFD 的记录
// @param gps GPS 子 IFD 的记录
// @return 完整的 TIFF 字节
func buildTIFF(ifd0, exif, gps []tiffEntry) []byte {
	exifOff := tiffHeaderLen + ifdSize(len(ifd0)+2)
	gpsOff := exifOff + ifdSize(len(exif))
	dataOff := gpsOff + ifdSize(len(gps))
	ifd0 = append(slices.Clone(ifd0), longEntry(tagExifIFD, uint32(exifOff)), longEntry(tagGPSIFD, uint32(gpsOff)))

	out := []byte{'M', 'M', 0, 0x2A, 0, 0, 0, tiffHeaderLen}
	var data []byte
	for _, entries := range [][]tiffEntry{ifd0, exif, gps} {
		out = appendIFD(out, &data, dataOff, entries)
	}
	return append(out, data...)
}

// appendIFD 向 out 追加一个 IFD:不超过 4 字节的值内联,更长的值追加到 data 并记下偏移。
// @param out 已写出的 TIFF 字节
// @param data 超过 4 字节的值的累积区,其起点在 TIFF 内的偏移为 dataOff
// @param dataOff data 区在 TIFF 内的起始偏移
// @param entries 本 IFD 的记录
// @return 追加后的 TIFF 字节
func appendIFD(out []byte, data *[]byte, dataOff int, entries []tiffEntry) []byte {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b tiffEntry) int { return int(a.tag) - int(b.tag) })
	out = binary.BigEndian.AppendUint16(out, uint16(len(sorted)))
	for _, e := range sorted {
		out = binary.BigEndian.AppendUint16(out, e.tag)
		out = binary.BigEndian.AppendUint16(out, e.typ)
		out = binary.BigEndian.AppendUint32(out, e.count)
		if len(e.value) <= 4 {
			out = append(out, e.value...)
			out = append(out, make([]byte, 4-len(e.value))...)
			continue
		}
		out = binary.BigEndian.AppendUint32(out, uint32(dataOff+len(*data)))
		*data = append(*data, e.value...)
		if len(*data)%2 == 1 {
			*data = append(*data, 0) // 值的起始偏移须为偶数
		}
	}
	return append(out, 0, 0, 0, 0) // 下一 IFD 偏移:没有后续 IFD
}
