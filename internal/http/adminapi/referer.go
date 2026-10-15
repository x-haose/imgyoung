package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/referer"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// refererBodyLimit 添加规则请求体上限。
const refererBodyLimit = 4 << 10

// refererItem 面向前端的防盗链规则项。
type refererItem struct {
	ID        int64  `json:"id"`         // 规则行 id,删除用
	Pattern   string `json:"pattern"`    // 规则:裸域或子域通配
	CreatedAt string `json:"created_at"` // 添加时刻
}

// HandleRefererList 返回全部防盗链规则(仅管理员路由)。
func (h *Handlers) HandleRefererList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.cfg.Referer.List(r.Context())
	if err != nil {
		h.cfg.Logger.Error("查询防盗链规则失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	items := make([]refererItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, refererItem{ID: row.ID, Pattern: row.Pattern, CreatedAt: row.CreatedAt})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: int64(len(items))})
}

// HandleRefererAdd 添加一条防盗链规则,即时生效(仅管理员路由)。
func (h *Handlers) HandleRefererAdd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, refererBodyLimit)
	var req struct {
		Pattern string `json:"pattern"` // 规则:裸域(example.com)或子域通配(*.example.com)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return
	}
	row, err := h.cfg.Referer.Add(r.Context(), req.Pattern)
	switch {
	case errors.Is(err, referer.ErrPatternInvalid):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.RefererPatternInvalid)
		return
	case errors.Is(err, referer.ErrPatternExists):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.RefererExists)
		return
	case err != nil:
		h.cfg.Logger.Error("添加防盗链规则失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionRefererAdd, row.Pattern, "")
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated,
		refererItem{ID: row.ID, Pattern: row.Pattern, CreatedAt: row.CreatedAt})
}

// HandleRefererDelete 删除一条防盗链规则,即时生效(仅管理员路由)。
func (h *Handlers) HandleRefererDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.RefererIDInvalid)
		return
	}
	pattern, err := h.cfg.Referer.Remove(r.Context(), id)
	switch {
	case errors.Is(err, referer.ErrPatternNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.RefererNotFound)
		return
	case err != nil:
		h.cfg.Logger.Error("删除防盗链规则失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionRefererRemove, pattern, "")
	w.WriteHeader(http.StatusNoContent)
}
