package adminapi

import (
	"fmt"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/alert"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// alertItem 面向前端的告警项。
type alertItem struct {
	ID         int64             `json:"id"`          // 告警主键
	Kind       string            `json:"kind"`        // 告警类型
	Target     string            `json:"target"`      // 告警对象,渠道名等;无对象为空串
	Message    string            `json:"message"`     // 中文描述
	Params     map[string]string `json:"params"`      // 描述参数,管理后台按类型与它渲染当前语言的描述;没有参数时为空对象
	OpenedAt   string            `json:"opened_at"`   // 开启时刻,UTC 文本
	ResolvedAt *string           `json:"resolved_at"` // 恢复时刻;null 表示仍未恢复
}

// alertItemFrom 把告警行翻译为响应项。
// @param a 告警行
// @return 面向前端的响应项
// @return 参数列无法解析时的错误
func alertItemFrom(a sqlc.Alert) (alertItem, error) {
	params, err := alert.DecodeParams(a.Params)
	if err != nil {
		return alertItem{}, fmt.Errorf("告警 %d: %w", a.ID, err)
	}
	return alertItem{
		ID: a.ID, Kind: a.Kind, Target: a.Target, Message: a.Message, Params: params,
		OpenedAt: a.OpenedAt, ResolvedAt: nullableStr(a.ResolvedAt),
	}, nil
}

// HandleAlertList 列出告警:缺省只给未恢复的全部;?status=all 分页给全部历史(含已恢复),最新在前。
func (h *Handlers) HandleAlertList(w http.ResponseWriter, r *http.Request) {
	// 分页参数无论是否用到都先校验:缺省模式不分页,但给出的非法值同样拒绝。
	all, err := flagParam(r.URL.Query(), "status", "all")
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	limit, offset, err := pagination(r)
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	var (
		rows  []sqlc.Alert
		total int64
	)
	if all {
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
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	items := make([]alertItem, 0, len(rows))
	for _, a := range rows {
		item, err := alertItemFrom(a)
		if err != nil {
			h.cfg.Logger.Error("还原告警失败", "err", err)
			httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}
