package adminapi

import (
	"net/http"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// alertItem 面向前端的告警项。
type alertItem struct {
	ID         int64   `json:"id"`          // 告警主键
	Kind       string  `json:"kind"`        // 告警类型
	Target     string  `json:"target"`      // 告警对象,渠道名等;无对象为空串
	Message    string  `json:"message"`     // 人话描述
	OpenedAt   string  `json:"opened_at"`   // 开启时刻,UTC 文本
	ResolvedAt *string `json:"resolved_at"` // 恢复时刻;null 表示仍未恢复
}

// alertItemFrom 把告警行翻译为响应项。
// @param a 告警行
// @return 面向前端的响应项
func alertItemFrom(a sqlc.Alert) alertItem {
	return alertItem{
		ID: a.ID, Kind: a.Kind, Target: a.Target, Message: a.Message,
		OpenedAt: a.OpenedAt, ResolvedAt: nullableStr(a.ResolvedAt),
	}
}

// HandleAlertList 列出告警:缺省只给未恢复的全部;?status=all 分页给全部历史(含已恢复),最新在前。
func (h *Handlers) HandleAlertList(w http.ResponseWriter, r *http.Request) {
	var (
		rows  []sqlc.Alert
		total int64
		err   error
	)
	if r.URL.Query().Get("status") == "all" {
		limit, offset := pagination(r)
		rows, err = h.cfg.Queries.ListAlertsPage(r.Context(), sqlc.ListAlertsPageParams{Limit: limit, Offset: offset})
		if err == nil {
			total, err = h.cfg.Queries.CountAlerts(r.Context())
		}
	} else {
		rows, err = h.cfg.Queries.ListOpenAlerts(r.Context())
		total = int64(len(rows))
	}
	if err != nil {
		h.cfg.Logger.Error("查询告警失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]alertItem, 0, len(rows))
	for _, a := range rows {
		items = append(items, alertItemFrom(a))
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}
