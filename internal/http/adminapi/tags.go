package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/tags"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// tagItem 当前用户的一个标签及其在用图片数。
type tagItem struct {
	Name       string `json:"name"`        // 标签名
	ImageCount int64  `json:"image_count"` // 带这个标签的图片数
}

// HandleTagList 列出当前用户正在使用的标签,按名称排序。
func (h *Handlers) HandleTagList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	rows, err := h.cfg.Queries.ListUserTags(r.Context(), p.UserID)
	if err != nil {
		h.cfg.Logger.Error("查询标签列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	items := make([]tagItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, tagItem{Name: row.Name, ImageCount: row.ImageCount})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: int64(len(items))})
}

// batchTagsRequest POST /images/batch-tags 的请求体。
type batchTagsRequest struct {
	IDs    []string `json:"ids"`    // 目标图片短 ID
	Add    []string `json:"add"`    // 追加的标签
	Remove []string `json:"remove"` // 移除的标签
}

// batchTagsResponse POST /images/batch-tags 的响应体。
type batchTagsResponse struct {
	Updated int                  `json:"updated"` // 成功修改的张数
	Failed  []batchDeleteFailure `json:"failed"`  // 失败明细;全部成功时为空数组
}

// HandleImageBatchTags 批量追加或移除图片标签:逐张修改,单张失败不中断其余;
// 他人的图一律按不存在处理,不泄露存在性。
func (h *Handlers) HandleImageBatchTags(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	var req batchTagsRequest
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 || len(req.IDs) > batchDeleteMax ||
		(len(req.Add) == 0 && len(req.Remove) == 0) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.BatchTagsInvalid,
			"max", strconv.Itoa(batchDeleteMax))
		return
	}
	add, err := tags.Normalize(req.Add)
	if err != nil {
		h.writeTagError(w, err)
		return
	}
	remove, err := tags.Normalize(req.Remove)
	if err != nil {
		h.writeTagError(w, err)
		return
	}
	resp := batchTagsResponse{Failed: []batchDeleteFailure{}}
	for _, imageID := range req.IDs {
		if e := h.tagOneOwned(r, p.IsAdmin(), p.UserID, imageID, tags.Change{Add: add, Remove: remove}); e != nil {
			resp.Failed = append(resp.Failed, batchDeleteFailure{ID: imageID, ErrorResponse: httpx.ErrorBody(e)})
			continue
		}
		resp.Updated++
	}
	h.recordAudit(r.Context(), audit.ActionImageTags, "batch",
		"images="+strconv.Itoa(len(req.IDs))+" add="+joinTags(add)+" remove="+joinTags(remove))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, resp)
}

// tagOneOwned 带归属校验修改一张图的标签。
// @param isAdmin 操作者是否管理员(管理员可改任何人的)
// @param userID 操作者用户 id
// @param imageID 目标图片短 ID
// @param c 修改内容
// @return 不可见/不存在或修改失败时的失败原因;成功为 nil
func (h *Handlers) tagOneOwned(r *http.Request, isAdmin bool, userID, imageID string, c tags.Change) *errcode.Error {
	owner, err := h.cfg.Queries.GetImageOwner(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !isAdmin && owner != userID) {
		return errcode.New(errcode.ImageNotFound)
	}
	if err != nil {
		h.cfg.Logger.Error("查询图片归属失败", "id", imageID, "err", err)
		return errcode.New(errcode.Internal)
	}
	switch err := h.cfg.Tags.Update(r.Context(), imageID, c); {
	case err == nil:
		return nil
	case errors.Is(err, tags.ErrImageNotFound):
		return errcode.New(errcode.ImageNotFound)
	case errors.Is(err, tags.ErrTooMany):
		return errcode.New(errcode.TagTooMany, "max", strconv.Itoa(tags.MaxPerImage))
	default:
		h.cfg.Logger.Error("修改图片标签失败", "id", imageID, "err", err)
		return errcode.New(errcode.Internal)
	}
}

// writeTagError 把标签服务的错误写成响应。
// @param err 标签服务返回的错误
func (h *Handlers) writeTagError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tags.ErrInvalidName):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.TagInvalid,
			"max", strconv.Itoa(tags.MaxNameRunes))
	case errors.Is(err, tags.ErrTooMany):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.TagTooMany,
			"max", strconv.Itoa(tags.MaxPerImage))
	case errors.Is(err, tags.ErrImageNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImageNotFound)
	default:
		h.cfg.Logger.Error("修改图片标签失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
	}
}

// joinTags 把标签名拼成审计说明里的文本。
// @param names 标签名
// @return 逗号分隔的文本
func joinTags(names []string) string {
	return strings.Join(names, ",")
}
