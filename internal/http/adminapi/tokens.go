package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// tokenNameMaxLen Token 用途名长度上限。
const tokenNameMaxLen = 64

// tokenItem 面向前端的 Token 列表项(仅识别前缀,无哈希无明文)。
type tokenItem struct {
	ID         int64   `json:"id"`           // Token 记录 id
	Name       string  `json:"name"`         // 用途名
	Prefix     string  `json:"prefix"`       // 明文前缀,识别用
	Scopes     string  `json:"scopes"`       // 授权范围
	LastUsedAt *string `json:"last_used_at"` // 最近使用时刻;null 表示从未使用
	RevokedAt  *string `json:"revoked_at"`   // 吊销时刻;null 表示有效
	CreatedAt  string  `json:"created_at"`   // 签发时刻
}

// HandleTokenList 列出 Token:默认本人;管理员可带 ?user_id= 查看指定用户。
func (h *Handlers) HandleTokenList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	target := p.UserID
	if q := r.URL.Query().Get("user_id"); q != "" && p.IsAdmin() {
		target = q
	}
	rows, err := h.cfg.Queries.ListTokensByUser(r.Context(), target)
	if err != nil {
		h.cfg.Logger.Error("查询 Token 列表失败", "user", target, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]tokenItem, 0, len(rows))
	for _, t := range rows {
		items = append(items, tokenItem{
			ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scopes: t.Scopes,
			LastUsedAt: nullableStr(t.LastUsedAt), RevokedAt: nullableStr(t.RevokedAt),
			CreatedAt: t.CreatedAt,
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: int64(len(items))})
}

// HandleTokenCreate 为当前用户签发具名 Token;明文仅在本响应返回一次。
func (h *Handlers) HandleTokenCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	var req struct {
		Name string `json:"name"` // 用途名
	}
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || len(req.Name) > tokenNameMaxLen {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体非法或名称为空/过长")
		return
	}
	issued, err := h.cfg.Auth.IssueToken(r.Context(), p.UserID, req.Name)
	switch {
	case errors.Is(err, auth.ErrTokenNameTaken):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "同名 Token 已存在")
		return
	case err != nil:
		h.cfg.Logger.Error("签发 Token 失败", "user", p.UserID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionTokenIssue, strconv.FormatInt(issued.ID, 10), "name="+req.Name)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated, map[string]any{
		"id":     issued.ID,
		"name":   req.Name,
		"prefix": issued.Prefix,
		"token":  issued.Plaintext,
	})
}

// HandleTokenRevoke 吊销 Token:本人可吊销自己的,管理员可吊销任何人的。
func (h *Handlers) HandleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	tokenID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "Token ID 非法")
		return
	}
	err = h.cfg.Auth.RevokeToken(r.Context(), p.UserID, tokenID)
	if errors.Is(err, auth.ErrTokenNotFound) && p.IsAdmin() {
		// 管理员路径:按记录归属者吊销。
		row, gerr := h.cfg.Queries.GetToken(r.Context(), tokenID)
		if gerr == nil {
			err = h.cfg.Auth.RevokeToken(r.Context(), row.UserID, tokenID)
		}
	}
	switch {
	case errors.Is(err, auth.ErrTokenNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "Token 不存在或已吊销")
		return
	case err != nil:
		h.cfg.Logger.Error("吊销 Token 失败", "id", tokenID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionTokenRevoke, strconv.FormatInt(tokenID, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

// nullableStr 把可空字符串列转为 JSON 友好的指针形态。
// @param v 可空字符串
// @return 有效时的指针,否则 nil
func nullableStr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
