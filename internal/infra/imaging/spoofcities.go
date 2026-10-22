package imaging

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

// City 伪装用的城市档案:位置取城市中心附近,时间按城市所在时区推算。
type City struct {
	ID      string  `json:"id"`      // 稳定标识,配置里引用它;发布后只增不删
	NameZH  string  `json:"name_zh"` // 后台显示的中文名
	NameEN  string  `json:"name_en"` // 后台显示的英文名
	Country string  `json:"country"` // 国家或地区代码,ISO 3166-1 alpha-2
	Lat     float64 `json:"lat"`     // 中心纬度,度,北正南负
	Lon     float64 `json:"lon"`     // 中心经度,度,东正西负
	Alt     int     `json:"alt"`     // 中心海拔,米,非负
	TZ      string  `json:"tz"`      // IANA 时区名
	Radius  int     `json:"radius"`  // 随机偏移半径,米;沿海与临河的城市取小,避免落进水里
}

// defaultRadius 城市中心的默认随机偏移半径,米;远大于一户住宅的尺度,小于城区。
const defaultRadius = 1500

// citiesJSON 内置城市档案的原始数据。
//
//go:embed spoofcities.json
var citiesJSON []byte

// cities 内置城市档案;中心点选在内陆的闹市区,偏移半径按到水域的距离收紧。
var cities = mustLoadCities()

// mustLoadCities 解析内置城市档案;数据随二进制嵌入,解析失败只可能是档案本身损坏。
// @return 城市切片
func mustLoadCities() []City {
	var list []City
	if err := json.Unmarshal(citiesJSON, &list); err != nil {
		panic("imaging: 内置城市档案损坏: " + err.Error())
	}
	return list
}

// Cities 返回内置城市档案。
// @return 城市切片(新分配,可自由修改)
func Cities() []City {
	return slices.Clone(cities)
}

// CityIDs 返回内置城市的标识。
// @return 标识切片
func CityIDs() []string {
	ids := make([]string, len(cities))
	for i, c := range cities {
		ids[i] = c.ID
	}
	return ids
}

// CountryCodes 返回内置城市覆盖的国家与地区代码,去重并按首次出现的顺序排列。
// @return 代码切片
func CountryCodes() []string {
	var codes []string
	for _, c := range cities {
		if !slices.Contains(codes, c.Country) {
			codes = append(codes, c.Country)
		}
	}
	return codes
}

// lookupCity 按标识取城市。
// @param id 城市标识
// @return 城市档案
// @return 标识不存在时为 ErrSpoofConfig
func lookupCity(id string) (City, error) {
	for _, c := range cities {
		if c.ID == id {
			return c, nil
		}
	}
	return City{}, fmt.Errorf("%w: 城市 %q", ErrSpoofConfig, id)
}

// radius 返回城市的随机偏移半径,未单独指定时取 defaultRadius。
// @return 半径,米
func (c City) radius() int {
	if c.Radius > 0 {
		return c.Radius
	}
	return defaultRadius
}
