package adminapi

import (
	"context"
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
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// imageItem 面向前端的图片列表/详情项。
type imageItem struct {
	ID             string  `json:"id"`              // 对外短 ID
	URL            string  `json:"url"`             // 访问路径
	UserID         string  `json:"user_id"`         // 归属用户
	TokenID        *int64  `json:"token_id"`        // 上传来源 Token;null 表示非 Token 途径或来源 Token 记录已删除
	TokenName      *string `json:"token_name"`      // 上传来源 Token 用途名;null 表示非 Token 途径或来源 Token 记录已删除
	FileName       string  `json:"file_name"`       // 原始文件名
	Mime           string  `json:"mime"`            // 真实 MIME
	Size           int64   `json:"size"`            // 存储字节
	Width          int64   `json:"width"`           // 像素宽
	Height         int64   `json:"height"`          // 像素高
	Sha256         string  `json:"sha256"`          // 内容哈希
	ExpireAt       *string `json:"expire_at"`       // 到期时刻;null 表示永久
	TTLSeconds     *int64  `json:"ttl_seconds"`     // 有效时长(秒),访问续期按此顺延;null 表示永久
	LastAccessAt   *string `json:"last_access_at"`  // 最近公开访问;null 表示从未
	AccessCount    int64   `json:"access_count"`    // 累计公开访问次数,flush 周期粒度(非精确 PV)
	PendingCleanup bool    `json:"pending_cleanup"` // 是否在待清理列表
	CleanupExempt  bool    `json:"cleanup_exempt"`  // 是否清理豁免
	CreatedAt      string  `json:"created_at"`      // 上传时刻
	RefCount       int64   `json:"ref_count"`       // 物理图引用计数(同图去重共享)
}

// replicaItem 单图在一个渠道上的副本状态。
type replicaItem struct {
	ChannelID   int64            `json:"channel_id"`   // 渠道主键
	ChannelName string           `json:"channel_name"` // 渠道名
	State       string           `json:"state"`        // 副本状态:pending / uploading / done / failed
	Attempts    int64            `json:"attempts"`     // 累计补齐尝试次数
	Failure     *channel.Failure `json:"failure"`      // 最近一次补齐失败的结论;nil 表示没有失败
	UpdatedAt   string           `json:"updated_at"`   // 状态最近更新时刻,UTC 文本
}

// imageDetail 图片详情:列表项字段之外附带副本分布。
type imageDetail struct {
	imageItem
	Replicas []replicaItem `json:"replicas"` // 物理图在各渠道的副本状态,按渠道优先级降序
}

// imageDetailFrom 把详情查询行与副本分布翻译为详情响应。
// @param ctx 上下文
// @param row 图片详情查询行
// @return 详情响应
// @return 副本查询失败时的错误
func (h *Handlers) imageDetailFrom(ctx context.Context, row sqlc.GetImageAdminRow) (imageDetail, error) {
	rows, err := h.cfg.Queries.ListImageReplicas(ctx, row.ID)
	if err != nil {
		return imageDetail{}, fmt.Errorf("查询副本分布: %w", err)
	}
	replicas := make([]replicaItem, 0, len(rows))
	for _, r := range rows {
		item := replicaItem{
			ChannelID: r.ChannelID, ChannelName: r.ChannelName, State: r.State,
			Attempts: r.Attempts, UpdatedAt: r.UpdatedAt,
		}
		if r.LastErrorReason != "" {
			item.Failure = &channel.Failure{Reason: channel.FailureReason(r.LastErrorReason), Detail: r.LastError}
		}
		replicas = append(replicas, item)
	}
	return imageDetail{imageItem: imageItemFrom(row), Replicas: replicas}, nil
}

// imageItemFrom 把查询行翻译为响应项(列表行与详情行字段同构,列表行经类型转换复用)。
// @param row 图片详情查询行
// @return 面向前端的响应项
func imageItemFrom(row sqlc.GetImageAdminRow) imageItem {
	return imageItem{
		ID: row.ID, URL: "/i/" + row.ID, UserID: row.UserID,
		TokenID: nullableInt(row.TokenID), TokenName: nullableStr(row.TokenName),
		FileName: row.FileName, Mime: row.Mime, Size: row.Size,
		Width: row.Width, Height: row.Height, Sha256: row.Sha256,
		ExpireAt: nullableStr(row.ExpireAt), TTLSeconds: nullableInt(row.TtlSeconds),
		LastAccessAt: nullableStr(row.LastPublicAccessAt), AccessCount: row.AccessCount,
		PendingCleanup: row.PendingCleanup != 0, CleanupExempt: row.CleanupExempt != 0,
		CreatedAt: row.CreatedAt, RefCount: row.RefCount,
	}
}

// HandleImageList 分页列出图片:普通用户只见自己的,管理员可按用户/Token/时间/关键词过滤。
func (h *Handlers) HandleImageList(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	q := r.URL.Query()
	userFilter := q.Get("user_id")
	if !p.IsAdmin() {
		userFilter = p.UserID
	}
	tokenID, _ := strconv.ParseInt(q.Get("token_id"), 10, 64)
	// exempt=1 只看已豁免;其余取值不过滤(豁免图闭环入口,配合详情抽屉的取消豁免)。
	exemptOnly := int64(0)
	if q.Get("exempt") == "1" {
		exemptOnly = 1
	}
	limit, offset := pagination(r)

	rows, err := h.cfg.Queries.ListImagesAdmin(r.Context(), sqlc.ListImagesAdminParams{
		UserID: userFilter, TokenID: tokenID, Search: q.Get("search"),
		Since: q.Get("since"), Until: q.Get("until"), Exempt: exemptOnly,
		PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		h.cfg.Logger.Error("查询图片列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	total, err := h.cfg.Queries.CountImagesAdmin(r.Context(), sqlc.CountImagesAdminParams{
		UserID: userFilter, TokenID: tokenID, Search: q.Get("search"),
		Since: q.Get("since"), Until: q.Get("until"), Exempt: exemptOnly,
	})
	if err != nil {
		h.cfg.Logger.Error("统计图片总数失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	items := make([]imageItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, imageItemFrom(sqlc.GetImageAdminRow(row)))
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// HandleImageGet 图片详情(含副本分布);普通用户只可见自己的(他人图一律 404,不泄露存在性)。
func (h *Handlers) HandleImageGet(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	imageID := chi.URLParam(r, "id")
	row, err := h.cfg.Queries.GetImageAdmin(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.IsAdmin() && row.UserID != p.UserID) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImageNotFound)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("查询图片详情失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.writeImageDetail(w, r, row)
}

// writeImageDetail 输出图片详情响应;副本查询失败记日志并回 500。
// @param row 图片详情查询行
func (h *Handlers) writeImageDetail(w http.ResponseWriter, r *http.Request, row sqlc.GetImageAdminRow) {
	detail, err := h.imageDetailFrom(r.Context(), row)
	if err != nil {
		h.cfg.Logger.Error("查询图片副本分布失败", "id", row.ID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, detail)
}

// imagePatchRequest PATCH /images/{id} 的请求体;指针字段缺省表示不修改该项。
type imagePatchRequest struct {
	TTLHours      *int64 `json:"ttl_hours"`      // >0 从现在起算新到期并更新时长;0 改永久
	CleanupExempt *bool  `json:"cleanup_exempt"` // 预设/取消清理豁免(仅管理员);取消时顺带摘 pending_cleanup 由下轮扫描重判
}

// HandleImagePatch 修改单图有效期或清理豁免;普通用户只可改自己的(他人图一律 404)。
// @Description: 清理豁免是冷图裁决的"保留"语义,属管理员裁决权,普通用户携带该项一律 403,
// 防止属主自助豁免绕过管理员的冷图回收管道;ttl_hours 对属主开放。
func (h *Handlers) HandleImagePatch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	imageID := chi.URLParam(r, "id")
	var req imagePatchRequest
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.TTLHours == nil && req.CleanupExempt == nil) ||
		(req.TTLHours != nil && (*req.TTLHours < 0 || *req.TTLHours > config.MaxDurationHours)) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.ImagePatchInvalid,
			"max", strconv.Itoa(config.MaxDurationHours))
		return
	}
	if req.CleanupExempt != nil && !p.IsAdmin() {
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, errcode.CleanupExemptAdminOnly)
		return
	}
	if _, ok := h.imageVisible(w, r, p, imageID); !ok {
		return
	}
	detail, err := h.applyImagePatch(r.Context(), imageID, req)
	if err != nil {
		h.cfg.Logger.Error("修改图片失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionImageUpdate, imageID, detail)

	row, err := h.cfg.Queries.GetImageAdmin(r.Context(), imageID)
	if err != nil {
		h.cfg.Logger.Error("回读图片详情失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.writeImageDetail(w, r, row)
}

// applyImagePatch 落库 PATCH 的修改项并拼审计说明。
// @Description: ttl_hours>0 → 到期 = 现在 + 时长并同步 ttl_seconds;=0 → 双列置空(永久)。
// 豁免开关无论方向都摘 pending_cleanup:置豁免即裁决保留,取消豁免交下轮冷图扫描重判。
// @param imageID 目标引用短 ID
// @param req 已通过校验的修改项
// @return 审计 detail(记录改了什么)
// @return 落库失败时的错误
func (h *Handlers) applyImagePatch(ctx context.Context, imageID string, req imagePatchRequest) (string, error) {
	detail := ""
	if req.TTLHours != nil {
		expireAt, ttlSeconds := sql.NullString{}, sql.NullInt64{}
		if *req.TTLHours > 0 {
			ttl := time.Duration(*req.TTLHours) * time.Hour
			expireAt = sql.NullString{String: sqlc.TimeText(time.Now().Add(ttl)), Valid: true}
			ttlSeconds = sql.NullInt64{Int64: int64(ttl / time.Second), Valid: true}
		}
		if _, err := h.cfg.Queries.UpdateImageExpiry(ctx, sqlc.UpdateImageExpiryParams{
			ExpireAt: expireAt, TtlSeconds: ttlSeconds, ID: imageID,
		}); err != nil {
			return "", fmt.Errorf("更新有效期: %w", err)
		}
		detail = fmt.Sprintf("ttl_hours=%d", *req.TTLHours)
	}
	if req.CleanupExempt != nil {
		exempt := int64(0)
		if *req.CleanupExempt {
			exempt = 1
		}
		if _, err := h.cfg.Queries.SetImageCleanupExempt(ctx, sqlc.SetImageCleanupExemptParams{
			Exempt: exempt, ID: imageID,
		}); err != nil {
			return "", fmt.Errorf("更新清理豁免: %w", err)
		}
		if detail != "" {
			detail += " "
		}
		detail += fmt.Sprintf("cleanup_exempt=%t", *req.CleanupExempt)
	}
	return detail, nil
}

// HandleImageDelete 删除图片引用;普通用户只可删自己的(他人图一律 404)。
func (h *Handlers) HandleImageDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	imageID := chi.URLParam(r, "id")
	owner, ok := h.imageVisible(w, r, p, imageID)
	if !ok {
		return
	}
	if err := h.cfg.Images.DeleteImage(r.Context(), imageID); err != nil {
		if errors.Is(err, lifecycle.ErrImageNotFound) {
			httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImageNotFound)
			return
		}
		h.cfg.Logger.Error("删除图片失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionImageDelete, imageID, "owner="+owner)
	w.WriteHeader(http.StatusNoContent)
}

// batchDeleteMax 单次批量删除的 id 数上限,防单请求长事务霸占单写者。
const batchDeleteMax = 100

// batchDeleteFailure 批量删除中单张失败的明细:引用短 ID 加上与错误信封同形的失败原因。
type batchDeleteFailure struct {
	ID                  string `json:"id"` // 失败的引用短 ID
	httpx.ErrorResponse        // 失败原因
}

// batchDeleteResponse POST /images/batch-delete 的响应体。
type batchDeleteResponse struct {
	Deleted int                  `json:"deleted"` // 成功删除张数
	Failed  []batchDeleteFailure `json:"failed"`  // 失败明细;全部成功时为空数组
}

// HandleImageBatchDelete 批量删除图片引用:逐张走单删的级联语义与权限
// (他人图一律按不存在处理,不泄露存在性),单张失败不中断其余。
func (h *Handlers) HandleImageBatchDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	var req struct {
		IDs []string `json:"ids"` // 待删除的引用短 ID 集
	}
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		len(req.IDs) == 0 || len(req.IDs) > batchDeleteMax {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.BatchIDsInvalid,
			"max", strconv.Itoa(batchDeleteMax))
		return
	}
	resp := batchDeleteResponse{Failed: []batchDeleteFailure{}}
	for _, imageID := range req.IDs {
		if e := h.deleteOneOwned(r.Context(), p.IsAdmin(), p.UserID, imageID); e != nil {
			resp.Failed = append(resp.Failed, batchDeleteFailure{ID: imageID, ErrorResponse: httpx.ErrorBody(e)})
			continue
		}
		resp.Deleted++
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, resp)
}

// deleteOneOwned 带归属校验删除一条引用并留审计。
// @param isAdmin 操作者是否管理员(管理员可删任何人的)
// @param userID 操作者用户 id
// @param imageID 目标引用短 ID
// @return 不可见/不存在或删除失败时的失败原因;成功为 nil
func (h *Handlers) deleteOneOwned(ctx context.Context, isAdmin bool, userID, imageID string) *errcode.Error {
	owner, err := h.cfg.Queries.GetImageOwner(ctx, imageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !isAdmin && owner != userID) {
		return errcode.New(errcode.ImageNotFound)
	}
	if err != nil {
		h.cfg.Logger.Error("查询图片归属失败", "id", imageID, "err", err)
		return errcode.New(errcode.Internal)
	}
	if err := h.cfg.Images.DeleteImage(ctx, imageID); err != nil {
		if errors.Is(err, lifecycle.ErrImageNotFound) {
			return errcode.New(errcode.ImageNotFound)
		}
		h.cfg.Logger.Error("删除图片失败", "id", imageID, "err", err)
		return errcode.New(errcode.Internal)
	}
	h.recordAudit(ctx, audit.ActionImageDelete, imageID, "owner="+owner)
	return nil
}

// imageVisible 校验目标图对操作者可见;不可见或查询失败时已写响应并返回 ok=false。
// @Description: 普通用户仅可见自己的,他人图一律 404 不泄露存在性(Get/Patch/Delete 共用的权限模式)。
// @param p 会话主体
// @param imageID 目标引用短 ID
// @return 归属用户 id(审计留痕用)
// @return 可见且可继续处理为 true
func (h *Handlers) imageVisible(w http.ResponseWriter, r *http.Request, p auth.Principal,
	imageID string) (string, bool) {
	owner, err := h.cfg.Queries.GetImageOwner(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.IsAdmin() && owner != p.UserID) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImageNotFound)
		return "", false
	}
	if err != nil {
		h.cfg.Logger.Error("查询图片归属失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return "", false
	}
	return owner, true
}
