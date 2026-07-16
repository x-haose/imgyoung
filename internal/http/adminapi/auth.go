package adminapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/http/middleware"
)

// loginBodyLimit 登录请求体上限。
const loginBodyLimit = 4 << 10

// loginRequest 登录请求体。
type loginRequest struct {
	Name     string `json:"name"`     // 登录名
	Password string `json:"password"` // 明文密码,仅在请求内存在
}

// sessionResponse 登录与 /me 的响应体:前端据此拿到主体与 CSRF 令牌。
type sessionResponse struct {
	User      userInfo `json:"user"`       // 会话主体
	CSRFToken string   `json:"csrf_token"` // 状态变更请求须回传的令牌
}

// userInfo 面向前端的用户概要。
type userInfo struct {
	ID   string `json:"id"`   // 用户 id
	Name string `json:"name"` // 登录名
	Role string `json:"role"` // 角色:admin / user
}

// HandleLogin 登录:校验凭证、创建会话并下发 cookie。
// @Description: 会话 cookie HttpOnly + SameSite=Lax,仅 TLS 请求带 Secure;
// CSRF 令牌经响应体交给前端,由后续请求以头回传。
func (h *Handlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, loginBodyLimit)
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	res, err := h.cfg.Auth.Login(r.Context(), req.Name, req.Password, clientIP(r))
	switch {
	case errors.Is(err, auth.ErrTooManyAttempts):
		httpx.WriteError(h.cfg.Logger, w, http.StatusTooManyRequests, "登录尝试过于频繁,请稍后再试")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnauthorized, "用户名或密码错误")
		return
	case errors.Is(err, auth.ErrUserDisabled):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, "该用户已被禁用")
		return
	case err != nil:
		h.cfg.Logger.Error("登录失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	http.SetCookie(w, sessionCookie(res.SessionID, r, sessionCookieMaxAge))
	h.cfg.Logger.Info("用户登录", "user", res.Principal.Name)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, sessionResponse{
		User:      userInfo{ID: res.Principal.UserID, Name: res.Principal.Name, Role: res.Principal.Role},
		CSRFToken: res.CSRFToken,
	})
}

// HandleLogout 登出:删除服务端会话并清除 cookie,幂等。
func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(middleware.SessionCookie); err == nil && c.Value != "" {
		if lerr := h.cfg.Auth.Logout(r.Context(), c.Value); lerr != nil {
			h.cfg.Logger.Error("删除会话失败", "err", lerr)
			httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
			return
		}
	}
	http.SetCookie(w, sessionCookie("", r, -1))
	w.WriteHeader(http.StatusNoContent)
}

// HandleMe 返回当前会话主体与 CSRF 令牌,前端刷新后据此恢复登录态。
func (h *Handlers) HandleMe(w http.ResponseWriter, r *http.Request) {
	info, ok := middleware.SessionFrom(r.Context())
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnauthorized, "未登录或会话已过期")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, sessionResponse{
		User:      userInfo{ID: info.Principal.UserID, Name: info.Principal.Name, Role: info.Principal.Role},
		CSRFToken: info.CSRFToken,
	})
}

// HandleChangePassword 当前用户改密:校验旧密码,成功后全部会话失效、须重新登录。
func (h *Handlers) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"` // 旧明文密码
		NewPassword string `json:"new_password"` // 新明文密码
	}
	r.Body = http.MaxBytesReader(w, r.Body, loginBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	err := h.cfg.Auth.ChangePassword(r.Context(), p.UserID, req.OldPassword, req.NewPassword)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "旧密码不正确")
		return
	case errors.Is(err, auth.ErrEmptyPassword):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "新密码不能为空")
		return
	case err != nil:
		h.cfg.Logger.Error("修改密码失败", "user", p.UserID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionUserUpdate, p.UserID, "password.change")
	http.SetCookie(w, sessionCookie("", r, -1))
	w.WriteHeader(http.StatusNoContent)
}

// sessionCookieMaxAge 会话 cookie 的存活秒数,与服务端会话有效期一致。
const sessionCookieMaxAge = 30 * 24 * 60 * 60

// sessionCookie 构造会话 cookie;maxAge 为负表示立即删除。
// @param value 会话 id 明文
// @param r 当前请求,用于判定是否加 Secure
// @param maxAge 存活秒数
// @return 待下发的 cookie
func sessionCookie(value string, r *http.Request, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     middleware.SessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

// clientIP 提取客户端 IP(不含端口),作登录限速键。
// @Description: 直接取连接对端地址;经反代部署时为反代地址,限速粒度随之放粗,
// 仍能阻止单点爆破。
// @param r 当前请求
// @return 客户端 IP 文本
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
