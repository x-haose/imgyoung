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
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// userBodyLimit 用户管理请求体上限。
const userBodyLimit = 16 << 10

// userItem 面向前端的用户列表项(不含任何凭证材料)。
type userItem struct {
	ID              string `json:"id"`                // 用户 id
	Name            string `json:"name"`              // 登录名
	Role            string `json:"role"`              // 角色:admin / user
	QuotaBytes      *int64 `json:"quota_bytes"`       // 配额上限;null 表示不限
	DefaultTTLHours *int64 `json:"default_ttl_hours"` // 上传默认有效期(小时);null 随全局,0 默认永久
	Disabled        bool   `json:"disabled"`          // 是否禁用
	CreatedAt       string `json:"created_at"`        // 创建时刻,UTC 文本
	UsedBytes       int64  `json:"used_bytes"`        // 已用字节,按引用计全额
	ImageCount      int64  `json:"image_count"`       // 图片数
}

// userWriteRequest 创建/更新用户的请求体。
type userWriteRequest struct {
	Name            string `json:"name"`              // 登录名
	Password        string `json:"password"`          // 初始密码,仅创建时使用
	Role            string `json:"role"`              // 角色,空则默认 user
	QuotaBytes      int64  `json:"quota_bytes"`       // 配额上限;<= 0 表示不限
	DefaultTTLHours *int64 `json:"default_ttl_hours"` // 上传默认有效期(小时);null 随全局,0 默认永久
	Disabled        bool   `json:"disabled"`          // 是否禁用,仅更新时使用
}

// HandleUserList 列出全部用户及其用量(仅管理员路由)。
func (h *Handlers) HandleUserList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.cfg.Queries.ListUsersWithUsage(r.Context())
	if err != nil {
		h.cfg.Logger.Error("查询用户列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	items := make([]userItem, 0, len(rows))
	for _, u := range rows {
		items = append(items, userItem{
			ID:              u.ID,
			Name:            u.Name,
			Role:            u.Role,
			QuotaBytes:      nullableInt(u.QuotaBytes),
			DefaultTTLHours: nullableInt(u.DefaultTtlHours),
			Disabled:        u.Disabled != 0,
			CreatedAt:       u.CreatedAt,
			UsedBytes:       u.UsedBytes,
			ImageCount:      u.ImageCount,
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: int64(len(items))})
}

// HandleUserCreate 创建用户(管理员建号)。
func (h *Handlers) HandleUserCreate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeUserBody(h, w, r)
	if !ok {
		return
	}
	role := req.Role
	if role == "" {
		role = auth.RoleUser
	}
	user, err := h.cfg.Auth.CreateUser(r.Context(), auth.CreateUserParams{
		Name: req.Name, Password: req.Password, Role: role,
		QuotaBytes: req.QuotaBytes, DefaultTTLHours: ttlNull(req.DefaultTTLHours),
	})
	switch {
	case errors.Is(err, auth.ErrUserNameTaken):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.UserNameTaken)
		return
	case errors.Is(err, auth.ErrInvalidRole):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidRole)
		return
	case errors.Is(err, auth.ErrPasswordTooShort):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.PasswordTooShort,
			"min", strconv.Itoa(auth.MinPasswordLength))
		return
	case err != nil:
		h.cfg.Logger.Error("创建用户失败", "name", req.Name, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionUserCreate, user.ID, "name="+user.Name+" role="+user.Role)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated, userItem{
		ID: user.ID, Name: user.Name, Role: user.Role,
		QuotaBytes: nullableInt(user.QuotaBytes), DefaultTTLHours: nullableInt(user.DefaultTtlHours),
		CreatedAt: user.CreatedAt,
	})
}

// HandleUserUpdate 更新用户资料(PUT 全量语义)。
func (h *Handlers) HandleUserUpdate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeUserBody(h, w, r)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "id")
	err := h.cfg.Auth.UpdateUser(r.Context(), userID, auth.UpdateUserParams{
		Name: req.Name, Role: req.Role, QuotaBytes: req.QuotaBytes,
		DefaultTTLHours: ttlNull(req.DefaultTTLHours), Disabled: req.Disabled,
	})
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.UserNotFound)
		return
	case errors.Is(err, auth.ErrUserProtected):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, errcode.UserProtected)
		return
	case errors.Is(err, auth.ErrUserNameTaken):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.UserNameTaken)
		return
	case errors.Is(err, auth.ErrInvalidRole):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidRole)
		return
	case err != nil:
		h.cfg.Logger.Error("更新用户失败", "id", userID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionUserUpdate, userID, "name="+req.Name)
	w.WriteHeader(http.StatusNoContent)
}

// HandleUserResetPassword 管理员重置指定用户密码(旧会话全部失效)。
func (h *Handlers) HandleUserResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"` // 新明文密码
	}
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return
	}
	userID := chi.URLParam(r, "id")
	if _, err := h.cfg.Queries.GetUser(r.Context(), userID); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.UserNotFound)
		return
	}
	err := h.cfg.Auth.SetPassword(r.Context(), userID, req.Password)
	switch {
	case errors.Is(err, auth.ErrPasswordTooShort):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.PasswordTooShort,
			"min", strconv.Itoa(auth.MinPasswordLength))
		return
	case err != nil:
		h.cfg.Logger.Error("重置密码失败", "id", userID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionUserUpdate, userID, "password.reset")
	w.WriteHeader(http.StatusNoContent)
}

// HandleUserDelete 删除用户;名下有图或属保护对象时拒绝。
func (h *Handlers) HandleUserDelete(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if p, ok := h.sessionPrincipal(r); ok && p.UserID == userID {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.CannotDeleteSelf)
		return
	}
	err := h.cfg.Auth.DeleteUser(r.Context(), userID)
	switch {
	case errors.Is(err, auth.ErrUserProtected):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, errcode.UserProtected)
		return
	case errors.Is(err, auth.ErrUserNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.UserNotFound)
		return
	case errors.Is(err, auth.ErrUserHasImages):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.UserHasImages)
		return
	case err != nil:
		h.cfg.Logger.Error("删除用户失败", "id", userID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionUserDelete, userID, "")
	w.WriteHeader(http.StatusNoContent)
}

// decodeUserBody 解析并校验用户写请求体,失败即写出 400。
// @return 请求体与是否成功
func decodeUserBody(h *Handlers, w http.ResponseWriter, r *http.Request) (userWriteRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req userWriteRequest
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return userWriteRequest{}, false
	}
	if v := req.DefaultTTLHours; v != nil && (*v < 0 || *v > config.MaxDurationHours) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UserTTLInvalid,
			"max", strconv.Itoa(config.MaxDurationHours))
		return userWriteRequest{}, false
	}
	return req, true
}

// ttlNull 把可空 TTL 指针转为列值(nil → NULL,随全局)。
// @param v 请求体指针
// @return 可空列值
func ttlNull(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// nullableInt 把可空整数列转为 JSON 友好的指针形态。
// @param v 可空整数
// @return 有效时的指针,否则 nil
func nullableInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
