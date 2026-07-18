package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/dedup"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// DedupAdmin 相似候选裁决执行口(由 dedup.Merger 实现)。
type DedupAdmin interface {
	// MergeCandidate 合并一个候选,保留 keepBlobID 一方。
	MergeCandidate(ctx context.Context, candidateID, keepBlobID int64) error
	// DismissCandidate 忽略一个候选,该对不再提示。
	DismissCandidate(ctx context.Context, candidateID int64) error
}

// candidateSide 候选对中一方的展示信息。
type candidateSide struct {
	BlobID   int64  `json:"blob_id"`  // 物理图 id(裁决保留方的标识)
	ImageID  string `json:"image_id"` // 该物理图任一引用的短 ID,预览用;空串表示暂无引用
	Mime     string `json:"mime"`     // 真实 MIME
	Size     int64  `json:"size"`     // 存储字节
	Width    int64  `json:"width"`    // 像素宽
	Height   int64  `json:"height"`   // 像素高
	RefCount int64  `json:"refs"`     // 引用数,合并后并入保留方
}

// candidateItem 面向前端的相似候选项。
type candidateItem struct {
	ID        int64         `json:"id"`         // 候选行 id
	Distance  int64         `json:"distance"`   // pHash 汉明距离
	CreatedAt string        `json:"created_at"` // 发现时刻
	A         candidateSide `json:"a"`          // 候选对 A(id 较小方)
	B         candidateSide `json:"b"`          // 候选对 B(id 较大方)
}

// HandleDedupCandidates 分页列出待裁决相似候选(仅管理员路由)。
func (h *Handlers) HandleDedupCandidates(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	rows, err := h.cfg.Queries.ListPendingDedupCandidateDetails(r.Context(),
		sqlc.ListPendingDedupCandidateDetailsParams{Limit: limit, Offset: offset})
	if err != nil {
		h.cfg.Logger.Error("查询相似候选失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	total, err := h.cfg.Queries.CountPendingDedupCandidates(r.Context())
	if err != nil {
		h.cfg.Logger.Error("统计相似候选失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]candidateItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, candidateItem{
			ID: row.ID, Distance: row.Distance, CreatedAt: row.CreatedAt,
			A: candidateSide{
				BlobID: row.BlobA, ImageID: row.ImageA, Mime: row.MimeA,
				Size: row.SizeA, Width: row.WidthA, Height: row.HeightA, RefCount: row.RefsA,
			},
			B: candidateSide{
				BlobID: row.BlobB, ImageID: row.ImageB, Mime: row.MimeB,
				Size: row.SizeB, Width: row.WidthB, Height: row.HeightB, RefCount: row.RefsB,
			},
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// HandleDedupMerge 裁决合并:保留指定一方,另一方并入(仅管理员路由)。
func (h *Handlers) HandleDedupMerge(w http.ResponseWriter, r *http.Request) {
	candidateID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "候选 ID 非法")
		return
	}
	var req struct {
		KeepBlobID int64 `json:"keep_blob_id"` // 保留的物理图 id
	}
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	err = h.cfg.Dedup.MergeCandidate(r.Context(), candidateID, req.KeepBlobID)
	switch {
	case errors.Is(err, dedup.ErrCandidateNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "候选不存在或已裁决")
		return
	case errors.Is(err, dedup.ErrBadKeeper):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "保留对象必须是候选对中的一方")
		return
	case errors.Is(err, dedup.ErrCandidateStale):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "候选涉及的图片已被删除,请刷新列表")
		return
	case err != nil:
		h.cfg.Logger.Error("合并相似候选失败", "candidate", candidateID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionDedupMerge, strconv.FormatInt(candidateID, 10),
		"keep_blob="+strconv.FormatInt(req.KeepBlobID, 10))
	w.WriteHeader(http.StatusNoContent)
}

// HandleDedupDismiss 裁决忽略:该候选对不再提示(仅管理员路由)。
func (h *Handlers) HandleDedupDismiss(w http.ResponseWriter, r *http.Request) {
	candidateID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "候选 ID 非法")
		return
	}
	err = h.cfg.Dedup.DismissCandidate(r.Context(), candidateID)
	switch {
	case errors.Is(err, dedup.ErrCandidateNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "候选不存在或已裁决")
		return
	case err != nil:
		h.cfg.Logger.Error("忽略相似候选失败", "candidate", candidateID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionDedupDismiss, strconv.FormatInt(candidateID, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

// cleanupItem 面向前端的待清理引用项。
type cleanupItem struct {
	ID           string  `json:"id"`             // 引用短 ID
	URL          string  `json:"url"`            // 访问路径,预览用
	UserID       string  `json:"user_id"`        // 归属用户
	FileName     string  `json:"file_name"`      // 原始文件名
	Mime         string  `json:"mime"`           // 真实 MIME
	Size         int64   `json:"size"`           // 存储字节
	Width        int64   `json:"width"`          // 像素宽
	Height       int64   `json:"height"`         // 像素高
	LastAccessAt *string `json:"last_access_at"` // 最近公开访问;null 表示从未
	CreatedAt    string  `json:"created_at"`     // 上传时刻
}

// HandleCleanupList 分页列出冷图待清理引用(仅管理员路由);"删除"裁决走图片删除接口。
func (h *Handlers) HandleCleanupList(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	rows, err := h.cfg.Queries.ListPendingCleanupImages(r.Context(),
		sqlc.ListPendingCleanupImagesParams{Limit: limit, Offset: offset})
	if err != nil {
		h.cfg.Logger.Error("查询待清理列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	total, err := h.cfg.Queries.CountPendingCleanupImages(r.Context())
	if err != nil {
		h.cfg.Logger.Error("统计待清理数失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	items := make([]cleanupItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, cleanupItem{
			ID: row.ID, URL: "/i/" + row.ID, UserID: row.UserID, FileName: row.FileName,
			Mime: row.Mime, Size: row.Size, Width: row.Width, Height: row.Height,
			LastAccessAt: nullableStr(row.LastPublicAccessAt), CreatedAt: row.CreatedAt,
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// HandleCleanupKeep 裁决保留:置清理豁免,该引用不再参与冷图打标(仅管理员路由)。
func (h *Handlers) HandleCleanupKeep(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "id")
	n, err := h.cfg.Queries.SetImageCleanupExempt(r.Context(), sqlc.SetImageCleanupExemptParams{Exempt: 1, ID: imageID})
	if err != nil {
		h.cfg.Logger.Error("清理豁免失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	if n == 0 {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "图片不存在")
		return
	}
	h.recordAudit(r.Context(), audit.ActionCleanupKeep, imageID, "")
	w.WriteHeader(http.StatusNoContent)
}
