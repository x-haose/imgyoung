package adminapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// imageItem 面向前端的图片列表/详情项。
type imageItem struct {
	ID             string  `json:"id"`                 // 对外短 ID
	URL            string  `json:"url"`                // 访问路径
	UserID         string  `json:"user_id"`            // 归属用户
	TokenID        *int64  `json:"token_id"`           // 上传来源 Token;null 表示非 Token 途径
	FileName       string  `json:"file_name"`          // 原始文件名
	Mime           string  `json:"mime"`               // 真实 MIME
	Size           int64   `json:"size"`               // 存储字节
	Width          int64   `json:"width"`              // 像素宽
	Height         int64   `json:"height"`             // 像素高
	Sha256         string  `json:"sha256"`             // 内容哈希
	ExpireAt       *string `json:"expire_at"`          // 到期时刻;null 表示永久
	LastAccessAt   *string `json:"last_access_at"`     // 最近公开访问;null 表示从未
	PendingCleanup bool    `json:"pending_cleanup"`    // 是否在待清理列表
	CleanupExempt  bool    `json:"cleanup_exempt"`     // 是否清理豁免
	CreatedAt      string  `json:"created_at"`         // 上传时刻
	RefCount       int64   `json:"ref_count,omitzero"` // 物理图引用计数,仅详情返回
}

// HandleImageList 分页列出图片:普通用户只见自己的,管理员可按用户/Token/时间/关键词过滤。
func (h *Handlers) HandleImageList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	q := r.URL.Query()
	userFilter := q.Get("user_id")
	if !p.IsAdmin() {
		userFilter = p.UserID
	}
	tokenID, _ := strconv.ParseInt(q.Get("token_id"), 10, 64)
	limit, offset := pagination(r)

	rows, err := h.cfg.Queries.ListImagesAdmin(r.Context(), sqlc.ListImagesAdminParams{
		UserID: userFilter, TokenID: tokenID, Search: q.Get("search"),
		Since: q.Get("since"), Until: q.Get("until"),
		PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		h.cfg.Logger.Error("查询图片列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	total, err := h.cfg.Queries.CountImagesAdmin(r.Context(), sqlc.CountImagesAdminParams{
		UserID: userFilter, TokenID: tokenID, Search: q.Get("search"),
		Since: q.Get("since"), Until: q.Get("until"),
	})
	if err != nil {
		h.cfg.Logger.Error("统计图片总数失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]imageItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, imageItem{
			ID: row.ID, URL: "/i/" + row.ID, UserID: row.UserID,
			TokenID: nullableInt(row.TokenID), FileName: row.FileName,
			Mime: row.Mime, Size: row.Size, Width: row.Width, Height: row.Height,
			Sha256: row.Sha256, ExpireAt: nullableStr(row.ExpireAt),
			LastAccessAt:   nullableStr(row.LastPublicAccessAt),
			PendingCleanup: row.PendingCleanup != 0, CleanupExempt: row.CleanupExempt != 0,
			CreatedAt: row.CreatedAt,
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// HandleImageGet 图片详情;普通用户只可见自己的(他人图一律 404,不泄露存在性)。
func (h *Handlers) HandleImageGet(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	imageID := chi.URLParam(r, "id")
	row, err := h.cfg.Queries.GetImageAdmin(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.IsAdmin() && row.UserID != p.UserID) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "图片不存在")
		return
	}
	if err != nil {
		h.cfg.Logger.Error("查询图片详情失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, imageItem{
		ID: row.ID, URL: "/i/" + row.ID, UserID: row.UserID,
		TokenID: nullableInt(row.TokenID), FileName: row.FileName,
		Mime: row.Mime, Size: row.Size, Width: row.Width, Height: row.Height,
		Sha256: row.Sha256, ExpireAt: nullableStr(row.ExpireAt),
		LastAccessAt:   nullableStr(row.LastPublicAccessAt),
		PendingCleanup: row.PendingCleanup != 0, CleanupExempt: row.CleanupExempt != 0,
		CreatedAt: row.CreatedAt, RefCount: row.RefCount,
	})
}

// HandleImageDelete 删除图片引用;普通用户只可删自己的(他人图一律 404)。
func (h *Handlers) HandleImageDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	imageID := chi.URLParam(r, "id")
	owner, err := h.cfg.Queries.GetImageOwner(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.IsAdmin() && owner != p.UserID) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "图片不存在")
		return
	}
	if err != nil {
		h.cfg.Logger.Error("查询图片归属失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	if err := h.cfg.Images.DeleteImage(r.Context(), imageID); err != nil {
		if errors.Is(err, lifecycle.ErrImageNotFound) {
			httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "图片不存在")
			return
		}
		h.cfg.Logger.Error("删除图片失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionImageDelete, imageID, "owner="+owner)
	w.WriteHeader(http.StatusNoContent)
}
