package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"slices"
	"testing"
)

// encodeJPEG 生成一张 2x2 的真实 JPEG 字节。
func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatalf("编码测试 JPEG: %v", err)
	}
	return buf.Bytes()
}

// spliceSegment 在 SOI 之后插入一个自定义 APP 段,模拟带元数据的 JPEG。
// @param marker 段标记(如 0xE1 = APP1)
// @param payload 段负载(不含标记与长度字段)
func spliceSegment(t *testing.T, jpg []byte, marker byte, payload []byte) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("测试输入不是合法 JPEG(缺 SOI)")
	}
	seg := make([]byte, 0, 4+len(payload))
	seg = append(seg, 0xFF, marker)
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	seg = append(seg, payload...)

	out := make([]byte, 0, len(jpg)+len(seg))
	out = append(out, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, jpg[2:]...)
	return out
}

// hasSegment 判断 JPEG 中是否存在指定标记的段(只扫描 SOS 之前的段表)。
func hasSegment(t *testing.T, jpg []byte, marker byte) bool {
	t.Helper()
	i := 2
	for i+4 <= len(jpg) {
		if jpg[i] != 0xFF {
			t.Fatalf("偏移 %d 处不是段边界: 0x%02X", i, jpg[i])
		}
		m := jpg[i+1]
		if m == 0xDA || m == 0xD9 { // SOS / EOI:段表结束
			return false
		}
		if m == marker {
			return true
		}
		segLen := int(binary.BigEndian.Uint16(jpg[i+2 : i+4]))
		i += 2 + segLen
	}
	return false
}

// TestStripJPEGMetadataRemovesMetadata 验证元数据段全部被剥离(白名单之外的
// APPn 与 COM),白名单段保留,且图片仍可解码。
func TestStripJPEGMetadataRemovesMetadata(t *testing.T) {
	jpg := encodeJPEG(t)
	jpg = spliceSegment(t, jpg, 0xE1,
		append([]byte("Exif\x00\x00"), bytes.Repeat([]byte{0xAB}, 32)...)) // APP1 Exif
	jpg = spliceSegment(t, jpg, 0xED, []byte("Photoshop 3.0\x00fake-iptc")) // APP13 IPTC
	jpg = spliceSegment(t, jpg, 0xEC, []byte("Ducky fake"))                 // APP12
	jpg = spliceSegment(t, jpg, 0xFE, []byte("created by some editor"))     // COM 注释
	jpg = spliceSegment(t, jpg, 0xE2, []byte("ICC_PROFILE\x00fake-icc"))    // APP2 ICC(应保留)

	got, err := StripJPEGMetadata(jpg)
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	for _, tc := range []struct {
		name   string
		marker byte
		want   bool
	}{
		{"APP1", 0xE1, false},
		{"APP13", 0xED, false},
		{"APP12", 0xEC, false},
		{"COM", 0xFE, false},
		{"APP2/ICC", 0xE2, true},
	} {
		if got, want := hasSegment(t, got, tc.marker), tc.want; got != want {
			t.Errorf("%s 段存在 = %v, 期望 %v", tc.name, got, want)
		}
	}
	if _, err := jpeg.Decode(bytes.NewReader(got)); err != nil {
		t.Fatalf("剥离后无法解码: %v", err)
	}
}

// TestStripJPEGMetadataKeepsPixels 验证剥离不改变像素数据(解码结果一致)。
func TestStripJPEGMetadataKeepsPixels(t *testing.T) {
	jpg := spliceSegment(t, encodeJPEG(t), 0xE1, []byte("Exif\x00\x00xxxx"))

	got, err := StripJPEGMetadata(jpg)
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	orig, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatalf("解码原图: %v", err)
	}
	stripped, err := jpeg.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("解码剥离后图: %v", err)
	}
	if orig.Bounds() != stripped.Bounds() {
		t.Fatalf("剥离改变了尺寸: %v → %v", orig.Bounds(), stripped.Bounds())
	}
	for y := range orig.Bounds().Dy() {
		for x := range orig.Bounds().Dx() {
			if orig.At(x, y) != stripped.At(x, y) {
				t.Fatalf("像素 (%d,%d) 不一致", x, y)
			}
		}
	}
}

// TestStripJPEGMetadataDropsTrailingData 验证 EOI 之后的尾部附加数据被丢弃。
// @Description: Motion Photo 等会在 JPEG 结尾附加视频/任意数据,隐私载荷
// 比 EXIF 更大,strip 必须一并截断。
func TestStripJPEGMetadataDropsTrailingData(t *testing.T) {
	jpg := encodeJPEG(t)
	withTrailer := slices.Concat(jpg, []byte("EMBEDDED-MP4-DATA-0123456789"))

	got, err := StripJPEGMetadata(withTrailer)
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	if bytes.Contains(got, []byte("EMBEDDED-MP4")) {
		t.Fatal("EOI 之后的尾部数据未被丢弃")
	}
	if len(got) < 2 || got[len(got)-2] != 0xFF || got[len(got)-1] != 0xD9 {
		t.Fatalf("输出未以 EOI(FFD9)结尾: 末尾字节 % X", got[max(0, len(got)-2):])
	}
	if _, err := jpeg.Decode(bytes.NewReader(got)); err != nil {
		t.Fatalf("截断后无法解码: %v", err)
	}
}

// TestStripJPEGMetadataSkipsPadding 验证段间的 0xFF 填充字节被规范化跳过。
func TestStripJPEGMetadataSkipsPadding(t *testing.T) {
	jpg := spliceSegment(t, encodeJPEG(t), 0xE1, []byte("Exif\x00\x00pad"))
	// 在 SOI 与首段之间插入两个填充 0xFF。
	padded := append([]byte{}, jpg[:2]...)
	padded = append(padded, 0xFF, 0xFF)
	padded = append(padded, jpg[2:]...)

	got, err := StripJPEGMetadata(padded)
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	if hasSegment(t, got, 0xE1) {
		t.Error("填充字节干扰了段剥离")
	}
	if _, err := jpeg.Decode(bytes.NewReader(got)); err != nil {
		t.Fatalf("剥离后无法解码: %v", err)
	}
}

// TestStripJPEGMetadataIdempotent 验证对已剥离的图再剥离,输出不变。
func TestStripJPEGMetadataIdempotent(t *testing.T) {
	once, err := StripJPEGMetadata(spliceSegment(t, encodeJPEG(t), 0xE1, []byte("Exif\x00\x00")))
	if err != nil {
		t.Fatalf("第一次剥离: %v", err)
	}
	twice, err := StripJPEGMetadata(once)
	if err != nil {
		t.Fatalf("第二次剥离: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatal("剥离不幂等:两次输出不一致")
	}
}

// TestStripJPEGMetadataRejectsMalformed 验证畸形输入返回错误而非崩溃或静默通过。
func TestStripJPEGMetadataRejectsMalformed(t *testing.T) {
	for name, input := range map[string][]byte{
		"非JPEG": []byte("not a jpeg at all"),
		"截断的段表": {0xFF, 0xD8, 0xFF, 0xE1, 0x7F, 0xFF, 0x00},
		"空输入":   {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := StripJPEGMetadata(input); err == nil {
				t.Fatal("畸形输入应返回错误")
			}
		})
	}
}

// exifWithOrientation 构造带方向与一段伪 GPS 数据的 Exif 段负载:方向记录之后跟着一个可识别的哨兵串。
func exifWithOrientation(o uint16) []byte {
	payload := append([]byte("Exif\x00\x00"), tiffOrientation(o)...)
	payload = binary.LittleEndian.AppendUint32(payload, 0) // 下一 IFD 偏移
	return append(payload, []byte("GPS-SENTINEL")...)
}

// TestStripJPEGMetadataKeepsOrientation 验证剥离后非默认方向仍在、其余 EXIF 数据不在,
// 默认方向不补段,且结果可解码、再剥一次不变。
func TestStripJPEGMetadataKeepsOrientation(t *testing.T) {
	jpg := spliceSegment(t, encodeJPEG(t), 0xE1, exifWithOrientation(6))
	got, err := StripJPEGMetadata(jpg)
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	if o := jpegOrientation(got); o != 6 {
		t.Fatalf("剥离后方向 = %d, 期望 6", o)
	}
	if bytes.Contains(got, []byte("GPS-SENTINEL")) {
		t.Fatal("方向之外的 EXIF 数据未被剥离")
	}
	if _, err := jpeg.Decode(bytes.NewReader(got)); err != nil {
		t.Fatalf("剥离后无法解码: %v", err)
	}
	again, err := StripJPEGMetadata(got)
	if err != nil || !bytes.Equal(again, got) {
		t.Fatalf("再剥一次结果变化(err=%v)", err)
	}

	normal, err := StripJPEGMetadata(spliceSegment(t, encodeJPEG(t), 0xE1, exifWithOrientation(1)))
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	if hasSegment(t, normal, 0xE1) {
		t.Fatal("默认方向不应补 Exif 段")
	}
}

// TestStripJPEGMetadataBetweenScans 验证扫描之间的元数据段(渐进式 JPEG 的形态)被剥离,
// 段里内嵌的 FFD9 不会让输出提前截断,内容不是 JFIF / ICC 的 APP0 与 APP2 同样被剥离。
func TestStripJPEGMetadataBetweenScans(t *testing.T) {
	jpg := encodeJPEG(t)
	eoi := len(jpg) - 2
	hidden := append([]byte("Exif\x00\x00GPS-SENTINEL"), 0xFF, 0xD9, 'x')
	seg := func(marker byte, payload []byte) []byte {
		b := []byte{0xFF, marker}
		b = binary.BigEndian.AppendUint16(b, uint16(len(payload)+2))
		return append(b, payload...)
	}
	in := slices.Concat(jpg[:eoi], seg(0xE1, hidden), seg(0xE0, []byte("JFXX\x00thumb")),
		seg(0xE2, []byte("MPF\x00more")), jpg[eoi:])

	got, err := StripJPEGMetadata(in)
	if err != nil {
		t.Fatalf("StripJPEGMetadata 返回错误: %v", err)
	}
	for _, leaked := range []string{"GPS-SENTINEL", "JFXX", "MPF"} {
		if bytes.Contains(got, []byte(leaked)) {
			t.Errorf("扫描之间的 %s 未被剥离", leaked)
		}
	}
	if !bytes.Equal(got, encodeJPEG(t)) {
		t.Error("剥离后应与不含元数据的原图逐字节相同")
	}
}
