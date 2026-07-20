package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// tokenNameMaxLen Token 用途名长度上限。
const tokenNameMaxLen = 64

// tokenExpiresMaxHours 有效期小时数上限(100 年),防 time.Duration 乘法溢出。
const tokenExpiresMaxHours = 876000

// tokenItem 面向前端的 Token 列表项(仅识别前缀,无哈希无明文)。
type tokenItem struct {
	ID         int64   `json:"id"`           // Token 记录 id
	Name       string  `json:"name"`         // 用途名
	Prefix     string  `json:"prefix"`       // 明文前缀,识别用
	Scopes     string  `json:"scopes"`       // 授权范围
	ExpiresAt  *string `json:"expires_at"`   // 到期时刻;null 表示永久
	LastUsedAt *string `json:"last_used_at"` // 最近使用时刻;null 表示从未使用
	RevokedAt  *string `json:"revoked_at"`   // 吊销时刻;null 表示未吊销
	CreatedAt  string  `json:"created_at"`   // 签发时刻
}

// HandleTokenList 分页列出 Token:默认本人;管理员可带 ?user_id= 查看指定用户。
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
	limit, offset := pagination(r)
	rows, err := h.cfg.Queries.ListTokensByUser(r.Context(), sqlc.ListTokensByUserParams{
		UserID: target, Limit: limit, Offset: offset,
	})
	if err != nil {
		h.cfg.Logger.Error("查询 Token 列表失败", "user", target, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	total, err := h.cfg.Queries.CountTokensByUser(r.Context(), target)
	if err != nil {
		h.cfg.Logger.Error("统计 Token 总数失败", "user", target, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]tokenItem, 0, len(rows))
	for _, t := range rows {
		items = append(items, tokenItem{
			ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scopes: t.Scopes,
			ExpiresAt: nullableStr(t.ExpiresAt), LastUsedAt: nullableStr(t.LastUsedAt),
			RevokedAt: nullableStr(t.RevokedAt), CreatedAt: t.CreatedAt,
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// HandleTokenCreate 为当前用户签发具名 Token;明文仅在本响应返回一次。
func (h *Handlers) HandleTokenCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	var req struct {
		Name           string `json:"name"`             // 用途名
		ExpiresInHours int64  `json:"expires_in_hours"` // 有效期小时数;缺省或 0 表示永久
	}
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Name == "" || len(req.Name) > tokenNameMaxLen ||
		req.ExpiresInHours < 0 || req.ExpiresInHours > tokenExpiresMaxHours {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest,
			"请求体非法:名称必填且不超长,有效期小时数需在 0 到 876000 之间")
		return
	}
	issued, err := h.cfg.Auth.IssueToken(r.Context(), p.UserID, req.Name,
		time.Duration(req.ExpiresInHours)*time.Hour)
	switch {
	case errors.Is(err, auth.ErrTokenNameTaken):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "同名 Token 已存在")
		return
	case err != nil:
		h.cfg.Logger.Error("签发 Token 失败", "user", p.UserID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionTokenIssue, strconv.FormatInt(issued.ID, 10),
		fmt.Sprintf("name=%s expires_in_hours=%d", req.Name, req.ExpiresInHours))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated, issuedTokenResponse(issued))
}

// HandleTokenRevoke 吊销 Token:本人可吊销自己的,管理员可吊销任何人的。
func (h *Handlers) HandleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	p, tokenID, ok := h.tokenRequest(w, r)
	if !ok {
		return
	}
	err := h.cfg.Auth.RevokeToken(r.Context(), h.resolveTokenOwner(r, p, tokenID), tokenID)
	if !h.writeTokenActionError(w, err, "吊销") {
		return
	}
	h.recordAudit(r.Context(), audit.ActionTokenRevoke, strconv.FormatInt(tokenID, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

// HandleTokenRotate 轮转 Token:同名新 Token 原子替换旧 Token,新明文仅此一次返回。
// 仅允许本人的有效 Token——轮转会下发新明文,跨用户轮转等同凭证接管,不开放给管理员。
func (h *Handlers) HandleTokenRotate(w http.ResponseWriter, r *http.Request) {
	p, tokenID, ok := h.tokenRequest(w, r)
	if !ok {
		return
	}
	issued, err := h.cfg.Auth.RotateToken(r.Context(), p.UserID, tokenID)
	if !h.writeTokenActionError(w, err, "轮转") {
		return
	}
	h.recordAudit(r.Context(), audit.ActionTokenRotate, strconv.FormatInt(tokenID, 10),
		fmt.Sprintf("name=%s new=%d", issued.Name, issued.ID))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, issuedTokenResponse(issued))
}

// HandleTokenDelete 删除已失效(已吊销或已过期)的 Token 记录:
// 本人可删自己的,管理员可删任何人的;有效 Token 返回 409。
func (h *Handlers) HandleTokenDelete(w http.ResponseWriter, r *http.Request) {
	p, tokenID, ok := h.tokenRequest(w, r)
	if !ok {
		return
	}
	err := h.cfg.Auth.DeleteTokenRecord(r.Context(), h.resolveTokenOwner(r, p, tokenID), tokenID)
	if !h.writeTokenActionError(w, err, "删除") {
		return
	}
	h.recordAudit(r.Context(), audit.ActionTokenDelete, strconv.FormatInt(tokenID, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

// tokenRequest 解析 Token 操作请求的会话主体与路径 id;失败时已写出响应。
// @param w 响应写出口
// @param r 请求
// @return 会话主体、Token id 与是否可继续
func (h *Handlers) tokenRequest(w http.ResponseWriter, r *http.Request) (auth.Principal, int64, bool) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return auth.Principal{}, 0, false
	}
	tokenID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "Token ID 非法")
		return auth.Principal{}, 0, false
	}
	return p, tokenID, true
}

// resolveTokenOwner 解析操作应作用的归属用户:管理员可操作任何人的 Token,
// 普通用户仅限本人;目标不存在时保持本人 id,由服务层归一为不存在。
// @param r 请求
// @param p 会话主体
// @param tokenID 目标 Token id
// @return 归属用户 id
func (h *Handlers) resolveTokenOwner(r *http.Request, p auth.Principal, tokenID int64) string {
	if !p.IsAdmin() {
		return p.UserID
	}
	row, err := h.cfg.Queries.GetToken(r.Context(), tokenID)
	if err != nil {
		return p.UserID
	}
	return row.UserID
}

// writeTokenActionError 把 Token 操作的领域错误映射为 HTTP 响应。
// @param w 响应写出口
// @param err 服务层返回的错误;nil 表示成功
// @param action 动作名,用于日志与文案
// @return 可继续写成功响应为 true;已写出错误响应为 false
func (h *Handlers) writeTokenActionError(w http.ResponseWriter, err error, action string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrTokenNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "Token 不存在或已失效")
	case errors.Is(err, auth.ErrTokenActive):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "Token 仍有效,请先吊销再删除")
	default:
		h.cfg.Logger.Error(action+" Token 失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
	}
	return false
}

// issuedTokenResponse 签发/轮转成功的统一响应体;token 字段为明文,仅此一次下发。
// @param issued 签发结果
// @return 待序列化的响应载荷
func issuedTokenResponse(issued auth.IssuedToken) map[string]any {
	return map[string]any{
		"id":     issued.ID,
		"name":   issued.Name,
		"prefix": issued.Prefix,
		"token":  issued.Plaintext,
	}
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
