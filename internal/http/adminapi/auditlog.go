package adminapi

import (
	"net/http"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// auditItem 面向前端的审计记录项。
type auditItem struct {
	ID        int64  `json:"id"`         // 记录 id
	Actor     string `json:"actor"`      // 操作者用户 id
	Action    string `json:"action"`     // 动作标识,形如 channel.create
	Target    string `json:"target"`     // 操作目标标识
	Detail    string `json:"detail"`     // 附加说明
	CreatedAt string `json:"created_at"` // 记录时刻
}

// HandleAuditList 分页查询审计日志,可按操作者/动作过滤(仅管理员路由)。
func (h *Handlers) HandleAuditList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := pagination(r)
	rows, err := h.cfg.Queries.ListAuditLog(r.Context(), sqlc.ListAuditLogParams{
		Actor: q.Get("actor"), Action: q.Get("action"),
		PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		h.cfg.Logger.Error("查询审计日志失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	total, err := h.cfg.Queries.CountAuditLog(r.Context(), sqlc.CountAuditLogParams{
		Actor: q.Get("actor"), Action: q.Get("action"),
	})
	if err != nil {
		h.cfg.Logger.Error("统计审计日志失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]auditItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, auditItem{
			ID: row.ID, Actor: row.Actor, Action: row.Action,
			Target: row.Target, Detail: row.Detail, CreatedAt: row.CreatedAt,
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}
