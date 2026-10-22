package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// tiffTypeSize 各 TIFF 字段类型的单元素字节数。
var tiffTypeSize = map[uint16]int{
	tiffByte: 1, tiffASCII: 1, tiffShort: 2, tiffLong: 4, tiffRational: 8, tiffUndefined: 1, tiffSRational: 8,
}

// readIFD 按 TIFF 规范独立解析一个 IFD,返回 tag → 值字节。
func readIFD(t *testing.T, tiff []byte, off int) map[uint16][]byte {
	t.Helper()
	out := map[uint16][]byte{}
	n := int(binary.BigEndian.Uint16(tiff[off:]))
	prev := -1
	for i := range n {
		e := tiff[off+2+i*ifdEntryLen:]
		tag := binary.BigEndian.Uint16(e)
		if int(tag) <= prev {
			t.Fatalf("IFD 记录未按 tag 升序: %#x 在 %#x 之后", tag, prev)
		}
		prev = int(tag)
		size := tiffTypeSize[binary.BigEndian.Uint16(e[2:])] * int(binary.BigEndian.Uint32(e[4:]))
		if size <= 4 {
			out[tag] = e[8 : 8+size]
			continue
		}
		valOff := int(binary.BigEndian.Uint32(e[8:]))
		if valOff%2 != 0 {
			t.Fatalf("tag %#x 的值偏移 %d 不是偶数", tag, valOff)
		}
		out[tag] = tiff[valOff : valOff+size]
	}
	return out
}

// spoofedEXIF 取出 JPEG 开头的 APP1/Exif 段,解析出三个 IFD。
func spoofedEXIF(t *testing.T, jpg []byte) (ifd0, exif, gps map[uint16][]byte) {
	t.Helper()
	tiff := findEXIFTIFF(jpg)
	if tiff == nil {
		t.Fatal("输出没有 EXIF 段")
	}
	ifd0 = readIFD(t, tiff, int(binary.BigEndian.Uint32(tiff[4:])))
	exif = readIFD(t, tiff, int(binary.BigEndian.Uint32(ifd0[tagExifIFD])))
	gps = readIFD(t, tiff, int(binary.BigEndian.Uint32(ifd0[tagGPSIFD])))
	return ifd0, exif, gps
}

// cstr 去掉 ASCII 值末尾的 NUL。
func cstr(b []byte) string { return strings.TrimRight(string(b), "\x00") }

// rat 读第 i 个无符号有理数。
func rat(b []byte, i int) float64 {
	return float64(binary.BigEndian.Uint32(b[i*8:])) / float64(binary.BigEndian.Uint32(b[i*8+4:]))
}

// dms 把 GPS 的度分秒三个有理数合成十进制度。
func dms(b []byte) float64 { return rat(b, 0) + rat(b, 1)/60 + rat(b, 2)/3600 }

// distanceMeters 两点间的近似距离(小范围内的平面近似)。
func distanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	dy := (lat1 - lat2) * metersPerDegree
	dx := (lon1 - lon2) * metersPerDegree * math.Cos(lat2*math.Pi/180)
	return math.Hypot(dx, dy)
}

// spoofInput 一张带原 GPS 与机型的 2x2 JPEG,方向标记为 6。
func spoofInput(t *testing.T) []byte {
	t.Helper()
	tiff := buildTIFF(
		[]tiffEntry{asciiEntry(0x010F, "REALMAKE"), shortEntry(exifOrientationTag, 6)},
		nil,
		[]tiffEntry{asciiEntry(0x0001, "N")},
	)
	payload := append(slices.Clone(exifPrefix), tiff...)
	return spliceSegment(t, encodeJPEG(t), markerAPP1, payload)
}

func newRNG(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed+1)) }

// TestSpoofJPEGConsistent 伪装后的 JPEG 仍可解码,原信息不残留,新信息彼此自洽。
func TestSpoofJPEGConsistent(t *testing.T) {
	in := spoofInput(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cfg := SpoofConfig{Uniform: true, CameraID: "apple-iphone-14", CityID: "tokyo", WindowDays: 30}
	out, err := Spoof(in, "image/jpeg", 2, 2, cfg, newRNG(1), now)
	if err != nil {
		t.Fatalf("Spoof: %v", err)
	}
	if bytes.Contains(out, []byte("REALMAKE")) {
		t.Fatal("原机型残留在输出里")
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("输出无法解码: %v", err)
	}
	ifd0, exif, gps := spoofedEXIF(t, out)
	if cstr(ifd0[0x010F]) != "Apple" || cstr(ifd0[0x0110]) != "iPhone 14" {
		t.Fatalf("机型 = %q %q", ifd0[0x010F], ifd0[0x0110])
	}
	if binary.BigEndian.Uint16(ifd0[exifOrientationTag]) != 6 {
		t.Fatal("方向标记没有保留")
	}
	if binary.BigEndian.Uint32(exif[0xA002]) != 2 || binary.BigEndian.Uint32(exif[0xA003]) != 2 {
		t.Fatal("像素尺寸与实际不符")
	}
	local, err := time.ParseInLocation(exifTimeLayout, cstr(exif[0x9003]), mustLoc(t, "Asia/Tokyo"))
	if err != nil {
		t.Fatalf("拍摄时间格式: %v", err)
	}
	age := now.Sub(local)
	if age < 0 || age > 31*24*time.Hour || local.Hour() < 8 || local.Hour() > 17 {
		t.Fatalf("拍摄时间 %v 不在窗口或不在白天", local)
	}
	if cstr(exif[0x9011]) != "+09:00" {
		t.Fatalf("时区偏移 = %q", exif[0x9011])
	}
	utc := local.UTC()
	if cstr(gps[0x001D]) != utc.Format("2006:01:02") || int(rat(gps[0x0007], 0)) != utc.Hour() {
		t.Fatal("GPS 的 UTC 时间与拍摄时间不一致")
	}
	city, _ := lookupCity("tokyo")
	d := distanceMeters(dms(gps[0x0002]), dms(gps[0x0004]), city.Lat, city.Lon)
	if cstr(gps[0x0001]) != "N" || cstr(gps[0x0003]) != "E" || d > float64(city.radius())+5 {
		t.Fatalf("GPS 位置偏离城市中心 %.0f 米", d)
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// TestSpoofDeterministic 同样的随机种子与时刻得到逐字节相同的输出。
func TestSpoofDeterministic(t *testing.T) {
	in := spoofInput(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cfg := SpoofConfig{WindowDays: 90}
	a, errA := Spoof(in, "image/jpeg", 2, 2, cfg, newRNG(7), now)
	b, errB := Spoof(in, "image/jpeg", 2, 2, cfg, newRNG(7), now)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		t.Fatalf("两次输出不同: %v %v", errA, errB)
	}
}

// TestSpoofNonJPEG 非 JPEG 按剥离处理,不写入假信息。
func TestSpoofNonJPEG(t *testing.T) {
	src := encodePNG(t)
	out, err := Spoof(src, "image/png", 2, 2, SpoofConfig{WindowDays: 1}, newRNG(1), time.Now())
	want, _ := StripPNGMetadata(src)
	if err != nil || !bytes.Equal(out, want) {
		t.Fatalf("PNG 应与剥离结果一致: %v", err)
	}
}

// TestSpoofRejectsUnknownProfile 固定档引用不存在的标识时失败,不回退到随机。
func TestSpoofRejectsUnknownProfile(t *testing.T) {
	in := spoofInput(t)
	for _, cfg := range []SpoofConfig{
		{Uniform: true, CameraID: "nope", CityID: "tokyo", WindowDays: 1},
		{Uniform: true, CameraID: "apple-iphone-14", CityID: "nope", WindowDays: 1},
	} {
		if _, err := Spoof(in, "image/jpeg", 2, 2, cfg, newRNG(1), time.Now()); !errors.Is(err, ErrSpoofConfig) {
			t.Fatalf("%+v 应当失败", cfg)
		}
	}
}

// TestPickProfileCountries 国家过滤只在候选范围内取城市;过滤后为空时失败。
func TestPickProfileCountries(t *testing.T) {
	rng := newRNG(3)
	for range 200 {
		_, city, err := pickProfile(SpoofConfig{Countries: []string{"HK", "MO"}}, rng)
		if err != nil || (city.Country != "HK" && city.Country != "MO") {
			t.Fatalf("取到 %+v, err=%v", city, err)
		}
	}
	if _, _, err := pickProfile(SpoofConfig{Countries: []string{"ZZ"}}, rng); !errors.Is(err, ErrSpoofConfig) {
		t.Fatal("没有候选城市时应失败")
	}
}

// TestCatalogInvariants 内置档案满足 Spoof 依赖的前提:标识唯一、时区可解析、海拔非负、参数非空。
func TestCatalogInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range cities {
		if seen[c.ID] {
			t.Fatalf("城市标识重复: %s", c.ID)
		}
		seen[c.ID] = true
		mustLoc(t, c.TZ)
		if c.Alt < 0 || len(c.Country) != 2 || math.Abs(c.Lat) > 90 || math.Abs(c.Lon) > 180 {
			t.Fatalf("城市 %s 的字段越界", c.ID)
		}
	}
	for _, c := range cameras {
		if seen[c.ID] {
			t.Fatalf("机型标识重复或与城市冲突: %s", c.ID)
		}
		seen[c.ID] = true
		if len(c.ISOs) == 0 || len(c.Shutters) == 0 || c.Make == "" || c.Model == "" {
			t.Fatalf("机型 %s 缺字段", c.ID)
		}
	}
}
