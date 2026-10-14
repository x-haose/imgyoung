package adminapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// exportPayload 元数据全量导出的结构:图在哪些渠道、key、哈希全部在内,
// 渠道凭证与密码哈希天然剔除——即便项目停更,持有者也能凭此自救。
type exportPayload struct {
	Users        []userItem         `json:"users"`         // 用户(无凭证材料)
	Channels     []channel.Info     `json:"channels"`      // 渠道描述(无配置与凭证)
	Tokens       []tokenExportItem  `json:"tokens"`        // Token 元数据(仅前缀)
	Images       []sqlc.Image       `json:"images"`        // 上传引用全量
	Blobs        []sqlc.Blob        `json:"blobs"`         // 物理图全量(含哈希与对象键)
	BlobChannels []sqlc.BlobChannel `json:"blob_channels"` // 副本分布全量
}

// tokenExportItem 导出的 Token 元数据(无哈希)。
type tokenExportItem struct {
	ID        int64   `json:"id"`         // 记录 id
	UserID    string  `json:"user_id"`    // 归属用户
	Name      string  `json:"name"`       // 用途名
	Prefix    string  `json:"prefix"`     // 明文前缀
	Scopes    string  `json:"scopes"`     // 授权范围
	RevokedAt *string `json:"revoked_at"` // 吊销时刻;null 表示有效
	CreatedAt string  `json:"created_at"` // 签发时刻
}

// HandleExport 导出全部元数据(仅管理员路由),渠道凭证脱敏。
func (h *Handlers) HandleExport(w http.ResponseWriter, r *http.Request) {
	payload, err := h.collectExport(r.Context())
	if err != nil {
		h.cfg.Logger.Error("导出元数据失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.recordAudit(r.Context(), audit.ActionDataExport, "", "")
	w.Header().Set("Content-Disposition", `attachment; filename="imgyoung-export.json"`)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, payload)
}

// collectExport 逐分区读出导出内容;任一分区失败整体失败,错误带分区名。
// @param ctx 上下文
// @return 导出内容
// @return 错误
func (h *Handlers) collectExport(ctx context.Context) (exportPayload, error) {
	users, err := h.cfg.Queries.ListUsersWithUsage(ctx)
	if err != nil {
		return exportPayload{}, fmt.Errorf("users: %w", err)
	}
	channels, err := h.cfg.Admin.List(ctx)
	if err != nil {
		return exportPayload{}, fmt.Errorf("channels: %w", err)
	}
	tokens, err := h.cfg.Queries.ListTokensExport(ctx)
	if err != nil {
		return exportPayload{}, fmt.Errorf("tokens: %w", err)
	}
	images, err := h.cfg.Queries.ListImagesExport(ctx)
	if err != nil {
		return exportPayload{}, fmt.Errorf("images: %w", err)
	}
	blobs, err := h.cfg.Queries.ListBlobsExport(ctx)
	if err != nil {
		return exportPayload{}, fmt.Errorf("blobs: %w", err)
	}
	blobChannels, err := h.cfg.Queries.ListBlobChannelsExport(ctx)
	if err != nil {
		return exportPayload{}, fmt.Errorf("blob_channels: %w", err)
	}
	return exportPayload{
		Users:        usersExport(users),
		Channels:     channels,
		Tokens:       tokensExport(tokens),
		Images:       images,
		Blobs:        blobs,
		BlobChannels: blobChannels,
	}, nil
}

// usersExport 把用户行映射为导出项(无凭证材料)。
// @param rows 用户与用量行
// @return 导出项
func usersExport(rows []sqlc.ListUsersWithUsageRow) []userItem {
	items := make([]userItem, 0, len(rows))
	for _, u := range rows {
		items = append(items, userItem{
			ID: u.ID, Name: u.Name, Role: u.Role, QuotaBytes: nullableInt(u.QuotaBytes),
			Disabled: u.Disabled != 0, CreatedAt: u.CreatedAt,
			UsedBytes: u.UsedBytes, ImageCount: u.ImageCount,
		})
	}
	return items
}

// tokensExport 把 Token 行映射为导出项(仅前缀,无哈希)。
// @param rows Token 导出行
// @return 导出项
func tokensExport(rows []sqlc.ListTokensExportRow) []tokenExportItem {
	items := make([]tokenExportItem, 0, len(rows))
	for _, t := range rows {
		items = append(items, tokenExportItem{
			ID: t.ID, UserID: t.UserID, Name: t.Name, Prefix: t.Prefix,
			Scopes: t.Scopes, RevokedAt: nullableStr(t.RevokedAt), CreatedAt: t.CreatedAt,
		})
	}
	return items
}
