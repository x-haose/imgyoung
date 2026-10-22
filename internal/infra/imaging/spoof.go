package imaging

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	// 内置时区数据库:城市所在时区在无系统 tzdata 的运行环境(distroless/static)里也能解析。
	_ "time/tzdata"
)

// SpoofConfig 伪装档的取值来源。
type SpoofConfig struct {
	Uniform    bool     // true 为全站固定机型与城市;false 为每张图独立随机
	CameraID   string   // Uniform 时使用的机型标识
	CityID     string   // Uniform 时使用的城市标识
	Countries  []string // 非 Uniform 时候选城市的国家代码;空表示全部城市
	WindowDays int      // 拍摄时间落在上传时刻往前这么多天之内,≥ 1
}

// exifTimeLayout EXIF 日期时间字段的格式。
const exifTimeLayout = "2006:01:02 15:04:05"

// metersPerDegree 地球表面一纬度对应的米数。
const metersPerDegree = 111320.0

// Spoof 把图片的元数据替换为一份自洽的假信息。
// @Description: JPEG 剥离全部原有元数据(含原 GPS、机型、时间)后,写入新生成的 EXIF:机型取自内置档案,
// 位置在城市中心附近随机偏移,拍摄时间落在最近 WindowDays 天内的当地白天,并与城市时区、GPS 的 UTC 时间一致。
// 宽高写入实际像素尺寸,保留原图的方向标记;不写缩略图与厂商私有数据。其余格式没有常见的 EXIF 承载,按剥离处理。
// 随机性与当前时刻由调用方注入,同样的输入得到同样的结果。
// @param data 完整图片字节
// @param mime 真实 MIME
// @param width 图片宽,像素
// @param height 图片高,像素
// @param cfg 机型与城市的取值来源
// @param rng 随机源
// @param now 当前时刻
// @return 处理后的字节
// @return 配置不可用时为 ErrSpoofConfig;输入图片畸形时为其他错误
func Spoof(data []byte, mime string, width, height int, cfg SpoofConfig, rng *rand.Rand,
	now time.Time) ([]byte, error) {
	if mime != "image/jpeg" {
		return StripMetadata(data, mime)
	}
	cam, city, err := pickProfile(cfg, rng)
	if err != nil {
		return nil, err
	}
	local, err := captureTime(city, cfg.WindowDays, rng, now)
	if err != nil {
		return nil, err
	}
	orientation := jpegOrientation(data)
	stripped, err := stripJPEG(data, false)
	if err != nil {
		return nil, err
	}
	tiff := buildTIFF(
		ifd0Entries(cam, local, orientation),
		exifEntries(cam, local, uint32(width), uint32(height), rng),
		gpsEntries(city, local, rng),
	)
	out := make([]byte, 0, len(stripped)+len(tiff)+len(exifPrefix)+4)
	out = append(out, stripped[:2]...)
	out = appendEXIFSegment(out, tiff)
	return append(out, stripped[2:]...), nil
}

// NewSpoofRNG 构造以系统熵播种的随机源,供 Spoof 在生产路径使用。
// @return 随机源;非并发安全,每次伪装各取一个
// @return 读取系统熵失败时的错误
func NewSpoofRNG() (*rand.Rand, error) {
	var seed [16]byte
	if _, err := cryptorand.Read(seed[:]); err != nil {
		return nil, fmt.Errorf("imaging: 读取系统熵: %w", err)
	}
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(seed[:8]), binary.BigEndian.Uint64(seed[8:]))), nil
}

// appendEXIFSegment 追加一个 APP1/Exif 段。
// @param out 已写出的字节
// @param tiff TIFF 字节,含头
// @return 追加后的字节
func appendEXIFSegment(out, tiff []byte) []byte {
	segLen := 2 + len(exifPrefix) + len(tiff)
	out = append(out, 0xFF, markerAPP1, byte(segLen>>8), byte(segLen))
	out = append(out, exifPrefix...)
	return append(out, tiff...)
}

// pickProfile 按配置选出机型与城市。
// @param cfg 取值来源
// @param rng 随机源
// @return 机型
// @return 城市
// @return 固定档引用了不存在的标识,或国家过滤后没有城市时的错误
func pickProfile(cfg SpoofConfig, rng *rand.Rand) (Camera, City, error) {
	if cfg.Uniform {
		cam, err := lookupCamera(cfg.CameraID)
		if err != nil {
			return Camera{}, City{}, err
		}
		city, err := lookupCity(cfg.CityID)
		return cam, city, err
	}
	var pool []City
	for _, c := range cities {
		if len(cfg.Countries) == 0 || containsString(cfg.Countries, c.Country) {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		return Camera{}, City{}, fmt.Errorf("%w: 国家过滤后没有候选城市", ErrSpoofConfig)
	}
	return cameras[rng.IntN(len(cameras))], pool[rng.IntN(len(pool))], nil
}

// containsString 判断切片是否含有 s。
// @param list 字符串切片
// @param s 待查值
// @return 含有为 true
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// captureTime 生成城市当地时区的拍摄时刻:最近 windowDays 天内的某天,当地 8 点至 17 点之间。
// @param city 城市
// @param windowDays 时间窗口,天,≥ 1
// @param rng 随机源
// @param now 当前时刻
// @return 带城市时区的拍摄时刻
// @return 时区无法解析时的错误(内置档案的不变量被破坏)
func captureTime(city City, windowDays int, rng *rand.Rand, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(city.TZ)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: 城市 %q 的时区 %q 无法解析: %w", ErrSpoofConfig, city.ID, city.TZ, err)
	}
	day := now.In(loc).AddDate(0, 0, -(1 + rng.IntN(windowDays)))
	return time.Date(day.Year(), day.Month(), day.Day(), 8+rng.IntN(10), rng.IntN(60), rng.IntN(60), 0, loc), nil
}

// utcOffset 把时刻的时区偏移写成 EXIF 的 ±HH:MM 形式。
// @param t 时刻
// @return 偏移文本
func utcOffset(t time.Time) string {
	_, sec := t.Zone()
	sign := '+'
	if sec < 0 {
		sign, sec = '-', -sec
	}
	return fmt.Sprintf("%c%02d:%02d", sign, sec/3600, sec%3600/60)
}

// ifd0Entries 生成 IFD0 的记录:机型、方向、分辨率、软件版本与修改时间。
// @param cam 机型
// @param local 拍摄时刻
// @param orientation 保留自原图的 EXIF 方向,[1,8]
// @return 记录
func ifd0Entries(cam Camera, local time.Time, orientation int) []tiffEntry {
	return []tiffEntry{
		asciiEntry(0x010F, cam.Make),
		asciiEntry(0x0110, cam.Model),
		shortEntry(exifOrientationTag, uint16(orientation)),
		rationalEntry(0x011A, [2]uint32{72, 1}),
		rationalEntry(0x011B, [2]uint32{72, 1}),
		shortEntry(0x0128, 2),
		asciiEntry(0x0131, cam.Software),
		asciiEntry(0x0132, local.Format(exifTimeLayout)),
		shortEntry(0x0213, 1),
	}
}

// exifEntries 生成 Exif 子 IFD 的记录:曝光参数、镜头、拍摄时间与像素尺寸。
// @param cam 机型
// @param local 拍摄时刻
// @param width 图片宽,像素
// @param height 图片高,像素
// @param rng 随机源
// @return 记录
func exifEntries(cam Camera, local time.Time, width, height uint32, rng *rand.Rand) []tiffEntry {
	stamp := local.Format(exifTimeLayout)
	offset := utcOffset(local)
	subsec := fmt.Sprintf("%02d", rng.IntN(100))
	entries := []tiffEntry{
		rationalEntry(0x829A, [2]uint32{1, cam.Shutters[rng.IntN(len(cam.Shutters))]}),
		rationalEntry(0x829D, cam.FNumber),
		shortEntry(0x8822, 2),
		shortEntry(0x8827, cam.ISOs[rng.IntN(len(cam.ISOs))]),
		undefinedEntry(0x9000, []byte("0232")),
		asciiEntry(0x9003, stamp),
		asciiEntry(0x9004, stamp),
		asciiEntry(0x9010, offset),
		asciiEntry(0x9011, offset),
		asciiEntry(0x9012, offset),
		undefinedEntry(0x9101, []byte{1, 2, 3, 0}),
		srationalEntry(0x9204, 0, 1),
		shortEntry(0x9207, 5),
		shortEntry(0x9209, 16),
		rationalEntry(0x920A, cam.Focal),
		asciiEntry(0x9290, subsec),
		asciiEntry(0x9291, subsec),
		asciiEntry(0x9292, subsec),
		undefinedEntry(0xA000, []byte("0100")),
		shortEntry(0xA001, 1),
		longEntry(0xA002, width),
		longEntry(0xA003, height),
		shortEntry(0xA402, 0),
		shortEntry(0xA403, 0),
		shortEntry(0xA405, cam.Focal35),
		shortEntry(0xA406, 0),
	}
	if cam.Lens != "" {
		entries = append(entries, asciiEntry(0xA434, cam.Lens))
	}
	return entries
}

// gpsEntries 生成 GPS 子 IFD 的记录:城市中心附近的随机位置、海拔与 UTC 时间。
// @param city 城市
// @param local 拍摄时刻
// @param rng 随机源
// @return 记录
func gpsEntries(city City, local time.Time, rng *rand.Rand) []tiffEntry {
	lat, lon := jitter(city, rng)
	latRef, lonRef := "N", "E"
	if lat < 0 {
		latRef = "S"
	}
	if lon < 0 {
		lonRef = "W"
	}
	utc := local.UTC()
	altitude := uint32(city.Alt+rng.IntN(21)) * 10
	return []tiffEntry{
		byteEntry(0x0000, 2, 3, 0, 0),
		asciiEntry(0x0001, latRef),
		rationalEntry(0x0002, degreesMinutesSeconds(lat)...),
		asciiEntry(0x0003, lonRef),
		rationalEntry(0x0004, degreesMinutesSeconds(lon)...),
		byteEntry(0x0005, 0),
		rationalEntry(0x0006, [2]uint32{altitude, 10}),
		rationalEntry(0x0007, [2]uint32{uint32(utc.Hour()), 1}, [2]uint32{uint32(utc.Minute()), 1},
			[2]uint32{uint32(utc.Second()), 1}),
		asciiEntry(0x001D, utc.Format("2006:01:02")),
	}
}

// jitter 在城市中心周围的圆盘内均匀取一个点。
// @param city 城市
// @param rng 随机源
// @return 纬度,度
// @return 经度,度
func jitter(city City, rng *rand.Rand) (lat, lon float64) {
	r := float64(city.radius()) * math.Sqrt(rng.Float64())
	theta := rng.Float64() * 2 * math.Pi
	lat = city.Lat + r*math.Cos(theta)/metersPerDegree
	lon = city.Lon + r*math.Sin(theta)/(metersPerDegree*math.Cos(city.Lat*math.Pi/180))
	return lat, lon
}

// degreesMinutesSeconds 把一个带符号的角度拆成 EXIF 的 度/分/秒 三个有理数,符号由调用方用 Ref 字段表达。
// @param v 角度,度
// @return 度、分、秒;秒保留四位小数
func degreesMinutesSeconds(v float64) [][2]uint32 {
	v = math.Abs(v)
	deg := math.Floor(v)
	minutes := math.Floor((v - deg) * 60)
	seconds := (v - deg - minutes/60) * 3600
	return [][2]uint32{{uint32(deg), 1}, {uint32(minutes), 1}, {uint32(seconds * 10000), 10000}}
}
