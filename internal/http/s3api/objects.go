package s3api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// HandleHeadObject 处理 HeadObject:只回对象的元数据头。
// @Description: 客户端上传后常以 HEAD 校验对象落地、按 Last-Modified 与 ETag 做同步判定。
func (h *Handlers) HandleHeadObject(w http.ResponseWriter, r *http.Request) {
	h.serveObject(w, r)
}

// HandleGetObject 处理 GetObject:流式输出对象字节与元数据头。
func (h *Handlers) HandleGetObject(w http.ResponseWriter, r *http.Request) {
	h.serveObject(w, r)
}

// serveObject 按签名主体与对象键定位引用并交对象输出口;HEAD 与 GET 只差输出口里是否开流。
// @param w 响应写入器
// @param r 请求
func (h *Handlers) serveObject(w http.ResponseWriter, r *http.Request) {
	principal, objectKey, ok := h.objectRequest(w, r)
	if !ok {
		return
	}
	imageID, err := h.cfg.Queries.GetImageIDByObjectKey(r.Context(), sqlc.GetImageIDByObjectKeyParams{
		UserID: principal.UserID, ObjectKey: sql.NullString{String: objectKey, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		h.writeNoSuchKey(w, r)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("按对象键定位引用失败", "object_key", objectKey, "err", err)
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
		return
	}
	switch err := h.cfg.Objects.ServeObject(w, r, imageID); {
	case err == nil:
	case errors.Is(err, publicapi.ErrObjectNotFound):
		h.writeNoSuchKey(w, r)
	case errors.Is(err, publicapi.ErrObjectUnavailable):
		h.writeStatus(w, r, http.StatusServiceUnavailable, "ServiceUnavailable", "渠道暂不可用")
	default:
		h.cfg.Logger.Error("输出 S3 对象失败", "id", imageID, "err", err)
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
	}
}

// HandleDeleteObject 处理 DeleteObject:删除签名主体名下该对象键的引用。
// @Description: 按 S3 语义幂等——对象不存在同样 204;他人的对象一律当作不存在,不泄露存在性。
// 删除走与后台相同的引用删除:引用计数、级联与审计照旧,审计主体为 Token 所属用户。
func (h *Handlers) HandleDeleteObject(w http.ResponseWriter, r *http.Request) {
	principal, objectKey, ok := h.objectRequest(w, r)
	if !ok {
		return
	}
	imageID, err := h.cfg.Queries.GetImageIDByObjectKey(r.Context(), sqlc.GetImageIDByObjectKeyParams{
		UserID: principal.UserID, ObjectKey: sql.NullString{String: objectKey, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("按对象键定位引用失败", "object_key", objectKey, "err", err)
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
		return
	}
	err = h.cfg.Images.DeleteImage(r.Context(), imageID)
	if err != nil && !errors.Is(err, lifecycle.ErrImageNotFound) {
		h.cfg.Logger.Error("S3 删除对象失败", "id", imageID, "err", err)
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
		return
	}
	if err := h.cfg.Audit.Record(r.Context(), principal.UserID, audit.ActionImageDelete, imageID,
		"s3 object_key="+objectKey); err != nil {
		h.cfg.Logger.Warn("审计记录失败", "action", audit.ActionImageDelete, "target", imageID, "err", err)
	}
	h.cfg.Logger.Info("S3 删除对象", "id", imageID, "object_key", objectKey)
	w.WriteHeader(http.StatusNoContent)
}

// objectRequest 取出请求的认证主体与对象键;缺主体属路由装配错误,键非法按 S3 错误码回复。
// @param w 响应写入器
// @param r 请求
// @return 认证主体
// @return 对象键
// @return 是否可继续;为 false 时已写出错误响应
func (h *Handlers) objectRequest(w http.ResponseWriter, r *http.Request) (auth.TokenPrincipal, string, bool) {
	principal, ok := middleware.TokenPrincipalFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("S3 对象请求缺少认证主体,检查路由装配")
		h.writeStatus(w, r, http.StatusInternalServerError, "InternalError", "服务内部错误")
		return auth.TokenPrincipal{}, "", false
	}
	objectKey, err := objectKeyFromPath(r.URL.EscapedPath())
	if err != nil {
		h.writeError(w, r, err)
		return auth.TokenPrincipal{}, "", false
	}
	return principal, objectKey, true
}

// writeNoSuchKey 回 S3 的对象不存在错误。
// @param w 响应写入器
// @param r 请求
func (h *Handlers) writeNoSuchKey(w http.ResponseWriter, r *http.Request) {
	h.writeStatus(w, r, http.StatusNotFound, "NoSuchKey", "对象不存在")
}
