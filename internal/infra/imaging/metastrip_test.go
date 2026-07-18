package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"

	_ "golang.org/x/image/webp" // 注册 webp 解码器,验证剥离后仍可被标准解码链解码
)

// encodePNG 生成一张 2x2 的真实 PNG 字节。
func encodePNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// buildPNGChunk 组装一个带正确 CRC 的 PNG chunk。
func buildPNGChunk(ctype string, payload []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(payload))) //nolint:gosec // 测试载荷为常量小字节
	out = append(out, ctype...)
	out = append(out, payload...)
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(ctype))
	_, _ = crc.Write(payload)
	return binary.BigEndian.AppendUint32(out, crc.Sum32())
}

// splicePNGChunks 在 IHDR 之后插入若干 chunk,模拟带元数据的 PNG。
func splicePNGChunks(t *testing.T, img []byte, chunks ...[]byte) []byte {
	t.Helper()
	// 签名 8 字节 + IHDR chunk 定长 25 字节(12 开销 + 13 载荷)。
	const ihdrEnd = 8 + 12 + 13
	if len(img) < ihdrEnd {
		t.Fatal("测试输入不是合法 PNG(IHDR 截断)")
	}
	out := make([]byte, 0, len(img)+256)
	out = append(out, img[:ihdrEnd]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, img[ihdrEnd:]...)
}

// hasPNGChunk 判断 PNG 中是否存在指定类型的 chunk。
func hasPNGChunk(t *testing.T, img []byte, ctype string) bool {
	t.Helper()
	for i := 8; i+12 <= len(img); {
		length := int(binary.BigEndian.Uint32(img[i : i+4]))
		if string(img[i+4:i+8]) == ctype {
			return true
		}
		i += 12 + length
	}
	return false
}

// encodeWebP 生成一张 2x2 的真实 WebP 字节(编码器产出 VP8X 打头的扩展容器)。
func encodeWebP(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := webp.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), webp.Options{Quality: 80, Method: 0}); err != nil {
		t.Fatalf("编码测试 WebP: %v", err)
	}
	return buf.Bytes()
}

// buildRIFFChunk 组装一个 RIFF chunk(奇数长度自动补偶数填充字节)。
func buildRIFFChunk(fourCC string, payload []byte) []byte {
	out := append([]byte{}, fourCC...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(payload))) //nolint:gosec // 测试载荷为常量小字节
	out = append(out, payload...)
	if len(payload)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

// wrapVP8XWithEXIF 在既有 VP8X 容器上置 EXIF 标志位并追加奇数长度的 EXIF chunk
// (兼验偶数填充处理),模拟带元数据的扩展 WebP。
func wrapVP8XWithEXIF(t *testing.T, src []byte) []byte {
	t.Helper()
	if !hasWebPChunk(t, src, webpChunkVP8X) || string(src[webpHeaderSize:webpHeaderSize+4]) != webpChunkVP8X {
		t.Fatal("测试编码器未产出 VP8X 打头的扩展容器")
	}
	out := append([]byte{}, src...)
	out[webpHeaderSize+8] |= vp8xFlagEXIF
	out = append(out, buildRIFFChunk(webpChunkEXIF, []byte("Exif\x00\x00fake-payload!"))...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8)) //nolint:gosec // 测试构造的容器远小于 uint32 上限
	return out
}

// hasWebPChunk 判断 WebP 容器中是否存在指定 FourCC 的 chunk。
func hasWebPChunk(t *testing.T, img []byte, fourCC string) bool {
	t.Helper()
	for i := webpHeaderSize; i+8 <= len(img); {
		size := int(binary.LittleEndian.Uint32(img[i+4 : i+8]))
		if string(img[i:i+4]) == fourCC {
			return true
		}
		i += 8 + size + size%2
	}
	return false
}

// TestStripMetadata 表驱动验证全格式元数据剥离:目标块消失、仍可被标准解码链
// 解码、像素尺寸不变;GIF 与未知格式直通。
func TestStripMetadata(t *testing.T) {
	cases := []struct {
		name  string
		mime  string
		build func(t *testing.T) []byte      // 构造带元数据的输入
		check func(t *testing.T, out []byte) // 格式特有断言(块不存在等)
	}{
		{
			name: "JPEG 剥 APP1 与 COM",
			mime: "image/jpeg",
			build: func(t *testing.T) []byte {
				jpg := encodeJPEG(t)
				jpg = spliceSegment(t, jpg, 0xE1, []byte("Exif\x00\x00fake"))
				return spliceSegment(t, jpg, 0xFE, []byte("comment"))
			},
			check: func(t *testing.T, out []byte) {
				if hasSegment(t, out, 0xE1) || hasSegment(t, out, 0xFE) {
					t.Error("剥离后仍存在 APP1/COM 段")
				}
			},
		},
		{
			name: "PNG 剥 eXIf 与文本块",
			mime: "image/png",
			build: func(t *testing.T) []byte {
				return splicePNGChunks(t, encodePNG(t),
					buildPNGChunk("eXIf", []byte("fake-exif")),
					buildPNGChunk("tEXt", []byte("Software\x00imgyoung-test")),
					buildPNGChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00fake")),
				)
			},
			check: func(t *testing.T, out []byte) {
				for _, ctype := range []string{"eXIf", "tEXt", "zTXt", "iTXt"} {
					if hasPNGChunk(t, out, ctype) {
						t.Errorf("剥离后仍存在 %s chunk", ctype)
					}
				}
			},
		},
		{
			name: "WebP 剥 EXIF 并清 VP8X 标志位",
			mime: "image/webp",
			build: func(t *testing.T) []byte {
				return wrapVP8XWithEXIF(t, encodeWebP(t))
			},
			check: func(t *testing.T, out []byte) {
				if hasWebPChunk(t, out, webpChunkEXIF) {
					t.Error("剥离后仍存在 EXIF chunk")
				}
				if !hasWebPChunk(t, out, webpChunkVP8X) {
					t.Fatal("VP8X 头不应被剥离")
				}
				if flags := out[webpHeaderSize+8]; flags&(vp8xFlagEXIF|vp8xFlagXMP) != 0 {
					t.Errorf("VP8X 标志位未清零: 0x%02X", flags)
				}
				if got := int(binary.LittleEndian.Uint32(out[4:8])); got != len(out)-8 {
					t.Errorf("RIFF 总长度字段 = %d, 期望 %d", got, len(out)-8)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.build(t)
			wantCfg, _, err := image.DecodeConfig(bytes.NewReader(in))
			if err != nil {
				t.Fatalf("测试输入本身不可解码: %v", err)
			}
			out, err := StripMetadata(in, tc.mime)
			if err != nil {
				t.Fatalf("StripMetadata: %v", err)
			}
			img, _, err := image.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("剥离后无法解码: %v", err)
			}
			if b := img.Bounds(); b.Dx() != wantCfg.Width || b.Dy() != wantCfg.Height {
				t.Errorf("剥离后尺寸 %dx%d, 期望 %dx%d", b.Dx(), b.Dy(), wantCfg.Width, wantCfg.Height)
			}
			tc.check(t, out)
		})
	}
}

// TestStripMetadataPassthrough 验证 GIF 与未知 MIME 原样直通(字节不变)。
func TestStripMetadataPassthrough(t *testing.T) {
	data := []byte("GIF89a-not-really-parsed")
	for _, mime := range []string{"image/gif", "application/octet-stream"} {
		out, err := StripMetadata(data, mime)
		if err != nil {
			t.Fatalf("StripMetadata(%s): %v", mime, err)
		}
		if !bytes.Equal(out, data) {
			t.Errorf("%s 未直通", mime)
		}
	}
}

// TestStripPNGDropsTrailingData 验证 IEND 之后的尾部附加数据被丢弃。
func TestStripPNGDropsTrailingData(t *testing.T) {
	in := append(encodePNG(t), []byte("stego-tail")...)
	out, err := StripPNGMetadata(in)
	if err != nil {
		t.Fatalf("StripPNGMetadata: %v", err)
	}
	if bytes.HasSuffix(out, []byte("stego-tail")) {
		t.Error("IEND 后的尾部数据未丢弃")
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("剥尾后无法解码: %v", err)
	}
}

// TestStripMalformedRejected 验证畸形输入快速失败而非静默产出坏字节。
func TestStripMalformedRejected(t *testing.T) {
	if _, err := StripPNGMetadata([]byte("not-a-png")); err == nil {
		t.Error("畸形 PNG 未报错")
	}
	if _, err := StripWebPMetadata([]byte("RIFF????NOPE")); err == nil {
		t.Error("畸形 WebP 未报错")
	}
	// 声明长度越界的 PNG chunk。
	bad := append(append([]byte{}, pngSignature...), buildPNGChunk("IHDR", []byte("short"))...)
	bad[8], bad[9], bad[10], bad[11] = 0xFF, 0xFF, 0xFF, 0xFF
	if _, err := StripPNGMetadata(bad); err == nil {
		t.Error("chunk 长度越界的 PNG 未报错")
	}
}
