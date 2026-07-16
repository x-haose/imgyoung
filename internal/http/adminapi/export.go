package adminapi

import (
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
	ctx := r.Context()
	usersRows, err := h.cfg.Queries.ListUsersWithUsage(ctx)
	if err != nil {
		h.exportFail(w, "users", err)
		return
	}
	channels, err := h.cfg.Admin.List(ctx)
	if err != nil {
		h.exportFail(w, "channels", err)
		return
	}
	tokens, err := h.cfg.Queries.ListTokensExport(ctx)
	if err != nil {
		h.exportFail(w, "tokens", err)
		return
	}
	images, err := h.cfg.Queries.ListImagesExport(ctx)
	if err != nil {
		h.exportFail(w, "images", err)
		return
	}
	blobs, err := h.cfg.Queries.ListBlobsExport(ctx)
	if err != nil {
		h.exportFail(w, "blobs", err)
		return
	}
	blobChannels, err := h.cfg.Queries.ListBlobChannelsExport(ctx)
	if err != nil {
		h.exportFail(w, "blob_channels", err)
		return
	}

	payload := exportPayload{
		Users:        make([]userItem, 0, len(usersRows)),
		Channels:     channels,
		Tokens:       make([]tokenExportItem, 0, len(tokens)),
		Images:       images,
		Blobs:        blobs,
		BlobChannels: blobChannels,
	}
	for _, u := range usersRows {
		payload.Users = append(payload.Users, userItem{
			ID: u.ID, Name: u.Name, Role: u.Role, QuotaBytes: nullableInt(u.QuotaBytes),
			Disabled: u.Disabled != 0, CreatedAt: u.CreatedAt,
			UsedBytes: u.UsedBytes, ImageCount: u.ImageCount,
		})
	}
	for _, t := range tokens {
		payload.Tokens = append(payload.Tokens, tokenExportItem{
			ID: t.ID, UserID: t.UserID, Name: t.Name, Prefix: t.Prefix,
			Scopes: t.Scopes, RevokedAt: nullableStr(t.RevokedAt), CreatedAt: t.CreatedAt,
		})
	}

	h.recordAudit(ctx, audit.ActionDataExport, "", "")
	w.Header().Set("Content-Disposition", `attachment; filename="imgyoung-export.json"`)
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, payload)
}

// exportFail 导出中途失败的统一出口。
// @param w 响应写入器
// @param part 失败的数据分区名
// @param err 底层错误
func (h *Handlers) exportFail(w http.ResponseWriter, part string, err error) {
	h.cfg.Logger.Error("导出元数据失败", "part", part, "err", err)
	httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
}
