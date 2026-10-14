package imaging

import (
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// tiffOrientation 构造一个仅含 Orientation tag 的最小 II(小端)TIFF 字节串。
func tiffOrientation(o uint16) []byte {
	b := make([]byte, 0, 22)
	b = append(b, 'I', 'I')                     // 小端
	b = binary.LittleEndian.AppendUint16(b, 42) // 魔数 0x002A
	b = binary.LittleEndian.AppendUint32(b, 8)  // IFD0 偏移
	b = binary.LittleEndian.AppendUint16(b, 1)  // entry 数
	b = binary.LittleEndian.AppendUint16(b, exifOrientationTag)
	b = binary.LittleEndian.AppendUint16(b, 3) // 类型 SHORT
	b = binary.LittleEndian.AppendUint32(b, 1) // count
	b = binary.LittleEndian.AppendUint16(b, o) // 值
	b = binary.LittleEndian.AppendUint16(b, 0) // 值字段余下 2 字节填充
	return b
}

// TestReadOrientation 验证从 TIFF 头读出 IFD0 的方向值,大小端各一份。
func TestReadOrientation(t *testing.T) {
	for o := uint16(1); o <= 8; o++ {
		if got := readOrientation(tiffOrientation(o)); got != int(o) {
			t.Errorf("orientation %d: got %d", o, got)
		}
	}
}

// TestReadOrientationMalformed 验证畸形 TIFF 一律退回正常方向而非报错。
func TestReadOrientationMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		{0x00},
		[]byte("II"), // 头不完整
		append([]byte("XX"), tiffOrientation(6)[2:]...), // 非法字节序标记
		tiffOrientation(99),                             // 越界方向值 → 归正常
	}
	for i, c := range cases {
		if got := readOrientation(c); got != orientationNormal {
			t.Errorf("case %d: 期望正常方向,得 %d", i, got)
		}
	}
}

// TestJPEGOrientationNonJPEG 验证非 JPEG 字节读方向得正常方向。
func TestJPEGOrientationNonJPEG(t *testing.T) {
	if got := jpegOrientation([]byte("not a jpeg")); got != orientationNormal {
		t.Errorf("非 JPEG 应回退正常方向,得 %d", got)
	}
	if got := jpegOrientation(nil); got != orientationNormal {
		t.Errorf("空输入应回退正常方向,得 %d", got)
	}
}

// TestApplyOrientationRotate90 用一张 2×1 的双色图验证顺时针 90(方向 6)后尺寸与像素落位。
func TestApplyOrientationRotate90(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	src.Set(0, 0, red)  // 左
	src.Set(1, 0, blue) // 右

	dst := applyOrientation(src, 6)
	b := dst.Bounds()
	if b.Dx() != 1 || b.Dy() != 2 {
		t.Fatalf("顺时针 90 应得 1×2,得 %dx%d", b.Dx(), b.Dy())
	}
	// 顺时针 90 后:原左(红)转到顶部,原右(蓝)转到底部。
	if r, _, _, _ := dst.At(0, 0).RGBA(); r>>8 != 255 {
		t.Error("顶部像素应为红")
	}
	if _, _, bl, _ := dst.At(0, 1).RGBA(); bl>>8 != 255 {
		t.Error("底部像素应为蓝")
	}
}

// TestApplyOrientationNormal 正常方向应原样返回,不复制。
func TestApplyOrientationNormal(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if applyOrientation(src, orientationNormal) != image.Image(src) {
		t.Error("正常方向应原样返回同一图像")
	}
}
