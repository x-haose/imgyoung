package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
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
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2)) //nolint:gosec // 测试载荷为几十字节常量,远小于 uint16 上限
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
	jpg = spliceSegment(t, jpg, 0xE1, append([]byte("Exif\x00\x00"), bytes.Repeat([]byte{0xAB}, 32)...)) // APP1 Exif
	jpg = spliceSegment(t, jpg, 0xED, []byte("Photoshop 3.0\x00fake-iptc"))                              // APP13 IPTC
	jpg = spliceSegment(t, jpg, 0xEC, []byte("Ducky fake"))                                              // APP12
	jpg = spliceSegment(t, jpg, 0xFE, []byte("created by some editor"))                                  // COM 注释
	jpg = spliceSegment(t, jpg, 0xE2, []byte("ICC_PROFILE\x00fake-icc"))                                 // APP2 ICC(应保留)

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
