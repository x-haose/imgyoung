package adminapi

import (
	"net/http"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// SpoofCameraView 伪装档可选机型。
type SpoofCameraView struct {
	ID    string `json:"id"`    // 机型标识,配置项 spoof_camera 的取值
	Label string `json:"label"` // 显示名
}

// SpoofCityView 伪装档可选城市。
type SpoofCityView struct {
	ID      string `json:"id"`      // 城市标识,配置项 spoof_city 的取值
	NameZH  string `json:"name_zh"` // 中文名
	NameEN  string `json:"name_en"` // 英文名
	Country string `json:"country"` // 所在国家或地区代码,ISO 3166-1 alpha-2
}

// SpoofProfilesView 伪装档可选的机型、城市与国家代码。
type SpoofProfilesView struct {
	Cameras   []SpoofCameraView `json:"cameras"`   // 内置机型
	Cities    []SpoofCityView   `json:"cities"`    // 内置城市
	Countries []string          `json:"countries"` // 城市覆盖的国家或地区代码,配置项 spoof_countries 的取值范围
}

// HandleSpoofProfiles 返回伪装档可选的机型、城市与国家代码(仅管理员路由),设置页据此填充选项。
func (h *Handlers) HandleSpoofProfiles(w http.ResponseWriter, _ *http.Request) {
	cams := imaging.Cameras()
	cities := imaging.Cities()
	view := SpoofProfilesView{
		Cameras:   make([]SpoofCameraView, len(cams)),
		Cities:    make([]SpoofCityView, len(cities)),
		Countries: imaging.CountryCodes(),
	}
	for i, c := range cams {
		view.Cameras[i] = SpoofCameraView{ID: c.ID, Label: c.Label}
	}
	for i, c := range cities {
		view.Cities[i] = SpoofCityView{ID: c.ID, NameZH: c.NameZH, NameEN: c.NameEN, Country: c.Country}
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, view)
}
