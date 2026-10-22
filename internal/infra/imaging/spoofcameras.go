package imaging

import (
	"errors"
	"fmt"
)

// Camera 伪装用的机型档案:一组自洽的厂商、型号、固件与镜头参数。
type Camera struct {
	ID       string    // 稳定标识,配置里引用它;不随显示名变化
	Label    string    // 后台显示名
	Make     string    // EXIF Make
	Model    string    // EXIF Model
	Software string    // EXIF Software:手机为系统版本,相机为固件版本
	Lens     string    // EXIF LensModel
	Focal    [2]uint32 // 镜头实际焦距(毫米),分子与分母
	FNumber  [2]uint32 // 光圈值,分子与分母
	Focal35  uint16    // 等效 35mm 焦距(毫米)
	ISOs     []uint16  // 随机取值的 ISO 档位
	Shutters []uint32  // 随机取值的快门分母,曝光时间为 1/x 秒
}

// ErrSpoofConfig 伪装配置不可用:引用了不存在的机型或城市、国家过滤后没有候选城市、或城市时区无法解析。
// 它表示配置或内置档案有误,与输入图片是否畸形无关。
var ErrSpoofConfig = errors.New("imaging: 伪装配置不可用")

// phoneISOs 手机机型随机取值的 ISO 档位。
var phoneISOs = []uint16{32, 40, 50, 64, 80, 100, 125, 160, 200, 400, 800}

// phoneShutters 手机机型随机取值的快门分母。
var phoneShutters = []uint32{30, 60, 120, 250, 500, 1000, 2000}

// bodyISOs 相机机身随机取值的 ISO 档位。
var bodyISOs = []uint16{100, 200, 400, 800, 1600, 3200}

// bodyShutters 相机机身随机取值的快门分母。
var bodyShutters = []uint32{60, 125, 250, 500, 1000, 2000, 4000}

// cameras 内置机型档案;新增或删除机型不影响已入库的图片,只影响之后的伪装与可选清单。
var cameras = []Camera{
	{
		ID: "apple-iphone-15-pro", Label: "Apple iPhone 15 Pro", Make: "Apple", Model: "iPhone 15 Pro",
		Software: "17.5.1", Lens: "iPhone 15 Pro back camera 6.765mm f/1.78",
		Focal: [2]uint32{6765, 1000}, FNumber: [2]uint32{178, 100}, Focal35: 24,
		ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "apple-iphone-14", Label: "Apple iPhone 14", Make: "Apple", Model: "iPhone 14",
		Software: "17.4", Lens: "iPhone 14 back dual wide camera 5.7mm f/1.5",
		Focal: [2]uint32{57, 10}, FNumber: [2]uint32{15, 10}, Focal35: 26, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "apple-iphone-13", Label: "Apple iPhone 13", Make: "Apple", Model: "iPhone 13",
		Software: "16.6", Lens: "iPhone 13 back dual wide camera 5.1mm f/1.6",
		Focal: [2]uint32{51, 10}, FNumber: [2]uint32{16, 10}, Focal35: 26, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "samsung-galaxy-s23-ultra", Label: "Samsung Galaxy S23 Ultra", Make: "samsung", Model: "SM-S918B",
		Software: "S918BXXU2AWK3", Lens: "",
		Focal: [2]uint32{63, 10}, FNumber: [2]uint32{17, 10}, Focal35: 23, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "samsung-galaxy-s21", Label: "Samsung Galaxy S21", Make: "samsung", Model: "SM-G991B",
		Software: "G991BXXU5CVF1", Lens: "",
		Focal: [2]uint32{54, 10}, FNumber: [2]uint32{18, 10}, Focal35: 26, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "google-pixel-7", Label: "Google Pixel 7", Make: "Google", Model: "Pixel 7",
		Software: "HDR+ 1.0.512348716zd", Lens: "",
		Focal: [2]uint32{681, 100}, FNumber: [2]uint32{185, 100}, Focal35: 25, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "xiaomi-13", Label: "Xiaomi 13", Make: "Xiaomi", Model: "2211133C",
		Software: "OS1.0.8.0.UMCCNXM", Lens: "",
		Focal: [2]uint32{56, 10}, FNumber: [2]uint32{18, 10}, Focal35: 23, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "xiaomi-12", Label: "Xiaomi 12", Make: "Xiaomi", Model: "2201123C",
		Software: "V14.0.4.0.TLACNXM", Lens: "",
		Focal: [2]uint32{56, 10}, FNumber: [2]uint32{19, 10}, Focal35: 26, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "huawei-p40", Label: "HUAWEI P40", Make: "HUAWEI", Model: "ANA-AN00",
		Software: "ANA-AN00 11.0.0.230", Lens: "",
		Focal: [2]uint32{55, 10}, FNumber: [2]uint32{19, 10}, Focal35: 27, ISOs: phoneISOs, Shutters: phoneShutters,
	},
	{
		ID: "canon-eos-5d-mark-iv", Label: "Canon EOS 5D Mark IV", Make: "Canon", Model: "Canon EOS 5D Mark IV",
		Software: "Firmware Version 1.3.4", Lens: "EF50mm f/1.8 STM",
		Focal: [2]uint32{50, 1}, FNumber: [2]uint32{18, 10}, Focal35: 50, ISOs: bodyISOs, Shutters: bodyShutters,
	},
	{
		ID: "sony-a7-iii", Label: "Sony α7 III", Make: "SONY", Model: "ILCE-7M3",
		Software: "ILCE-7M3 v3.10", Lens: "FE 35mm F1.8",
		Focal: [2]uint32{35, 1}, FNumber: [2]uint32{18, 10}, Focal35: 35, ISOs: bodyISOs, Shutters: bodyShutters,
	},
	{
		ID: "nikon-d750", Label: "Nikon D750", Make: "NIKON CORPORATION", Model: "NIKON D750",
		Software: "Ver.1.11", Lens: "AF-S NIKKOR 50mm f/1.8G",
		Focal: [2]uint32{50, 1}, FNumber: [2]uint32{18, 10}, Focal35: 50, ISOs: bodyISOs, Shutters: bodyShutters,
	},
	{
		ID: "fujifilm-x-t4", Label: "Fujifilm X-T4", Make: "FUJIFILM", Model: "X-T4",
		Software: "Digital Camera X-T4 Ver1.20", Lens: "XF35mmF2 R WR",
		Focal: [2]uint32{35, 1}, FNumber: [2]uint32{20, 10}, Focal35: 53, ISOs: bodyISOs, Shutters: bodyShutters,
	},
}

// Cameras 返回内置机型档案。
// @return 机型切片(新分配,可自由修改)
func Cameras() []Camera {
	out := make([]Camera, len(cameras))
	copy(out, cameras)
	return out
}

// CameraIDs 返回内置机型的标识。
// @return 标识切片
func CameraIDs() []string {
	ids := make([]string, len(cameras))
	for i, c := range cameras {
		ids[i] = c.ID
	}
	return ids
}

// lookupCamera 按标识取机型。
// @param id 机型标识
// @return 机型档案
// @return 标识不存在时为 ErrSpoofConfig
func lookupCamera(id string) (Camera, error) {
	for _, c := range cameras {
		if c.ID == id {
			return c, nil
		}
	}
	return Camera{}, fmt.Errorf("%w: 机型 %q", ErrSpoofConfig, id)
}
