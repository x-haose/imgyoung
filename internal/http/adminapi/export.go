package adminapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// exportPayload 元数据全量导出的结构:图在哪些渠道、key、哈希全部在内,
// 渠道凭证与密码哈希天然剔除——即便项目停更,持有者也能凭此自救。
type exportPayload struct {
	Users        []userItem              `json:"users"`         // 用户(无凭证材料)
	Channels     []channel.Info          `json:"channels"`      // 渠道描述(无配置与凭证)
	Tokens       []tokenExportItem       `json:"tokens"`        // Token 元数据(仅前缀)
	Images       []imageExportItem       `json:"images"`        // 上传引用全量
	Blobs        []blobExportItem        `json:"blobs"`         // 物理图全量(含哈希与对象键)
	BlobChannels []blobChannelExportItem `json:"blob_channels"` // 副本分布全量
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

// imageExportItem 导出的上传引用。
type imageExportItem struct {
	ID                 string  `json:"id"`                    // 引用短 ID
	BlobID             int64   `json:"blob_id"`               // 物理图主键
	UserID             string  `json:"user_id"`               // 归属用户
	TokenID            *int64  `json:"token_id"`              // 来源 Token;null 表示非 Token 途径或 Token 记录已删除
	FileName           string  `json:"file_name"`             // 原始文件名
	ObjectKey          *string `json:"object_key"`            // S3 客户端自取的对象键;null 表示无别名
	ExpireAt           *string `json:"expire_at"`             // 到期时刻,UTC 文本;null 表示永久
	TTLSeconds         *int64  `json:"ttl_seconds"`           // 有效时长(秒);null 表示永久
	LastPublicAccessAt *string `json:"last_public_access_at"` // 最近公开访问,UTC 文本;null 表示从未
	AccessCount        int64   `json:"access_count"`          // 累计公开访问次数
	PendingCleanup     bool    `json:"pending_cleanup"`       // 是否在待清理列表
	CleanupExempt      bool    `json:"cleanup_exempt"`        // 是否清理豁免
	CreatedAt          string  `json:"created_at"`            // 上传时刻,UTC 文本
}

// blobExportItem 导出的物理图。
type blobExportItem struct {
	ID            int64   `json:"id"`             // 物理图主键
	Sha256        string  `json:"sha256"`         // 内容哈希
	Phash         string  `json:"phash"`          // 64 位感知哈希,16 位十六进制;JSON 数字在 JavaScript 里只有 53 位精度
	StorageKey    string  `json:"storage_key"`    // 各渠道内的对象键
	Mime          string  `json:"mime"`           // 真实 MIME
	Size          int64   `json:"size"`           // 字节数
	Width         int64   `json:"width"`          // 像素宽
	Height        int64   `json:"height"`         // 像素高
	RefCount      int64   `json:"ref_count"`      // 引用计数
	PendingDelete bool    `json:"pending_delete"` // 是否已进入物理删除流程
	DeleteAfter   *string `json:"delete_after"`   // 物理删除不早于该时刻,UTC 文本;null 表示未排程
	CreatedAt     string  `json:"created_at"`     // 入库时刻,UTC 文本
}

// blobChannelExportItem 导出的副本分布行。
type blobChannelExportItem struct {
	BlobID          int64  `json:"blob_id"`           // 物理图主键
	ChannelID       int64  `json:"channel_id"`        // 渠道主键
	State           string `json:"state"`             // 副本状态:pending / uploading / done / failed
	Attempts        int64  `json:"attempts"`          // 累计补齐尝试次数
	LastErrorReason string `json:"last_error_reason"` // 最近一次失败的归类;空表示没有失败
	LastError       string `json:"last_error"`        // 最近一次失败的根因原文;空表示没有失败
	UpdatedAt       string `json:"updated_at"`        // 状态最近更新时刻,UTC 文本
}

// HandleExport 导出全部元数据(仅管理员路由),渠道凭证脱敏。
func (h *Handlers) HandleExport(w http.ResponseWriter, r *http.Request) {
	payload, err := h.collectExport(r.Context())
	if err != nil {
		h.cfg.Logger.Error("导出元数据失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
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
		Images:       imagesExport(images),
		Blobs:        blobsExport(blobs),
		BlobChannels: blobChannelsExport(blobChannels),
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
			DefaultTTLHours: nullableInt(u.DefaultTtlHours), Disabled: u.Disabled != 0, CreatedAt: u.CreatedAt,
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

// imagesExport 把上传引用行映射为导出项,可空列以 null 表达。
// @param rows 引用行
// @return 导出项
func imagesExport(rows []sqlc.Image) []imageExportItem {
	items := make([]imageExportItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, imageExportItem{
			ID: r.ID, BlobID: r.BlobID, UserID: r.UserID, TokenID: nullableInt(r.TokenID),
			FileName: r.FileName, ObjectKey: nullableStr(r.ObjectKey), ExpireAt: nullableStr(r.ExpireAt),
			TTLSeconds: nullableInt(r.TtlSeconds), LastPublicAccessAt: nullableStr(r.LastPublicAccessAt),
			AccessCount: r.AccessCount, PendingCleanup: r.PendingCleanup != 0, CleanupExempt: r.CleanupExempt != 0,
			CreatedAt: r.CreatedAt,
		})
	}
	return items
}

// blobsExport 把物理图行映射为导出项。
// @param rows 物理图行
// @return 导出项
func blobsExport(rows []sqlc.Blob) []blobExportItem {
	items := make([]blobExportItem, 0, len(rows))
	for _, b := range rows {
		items = append(items, blobExportItem{
			ID: b.ID, Sha256: b.Sha256, Phash: imaging.PHashHex(b.Phash), StorageKey: b.StorageKey, Mime: b.Mime,
			Size: b.Size, Width: b.Width, Height: b.Height, RefCount: b.RefCount,
			PendingDelete: b.PendingDelete != 0, DeleteAfter: nullableStr(b.DeleteAfter), CreatedAt: b.CreatedAt,
		})
	}
	return items
}

// blobChannelsExport 把副本分布行映射为导出项。
// @param rows 副本分布行
// @return 导出项
func blobChannelsExport(rows []sqlc.BlobChannel) []blobChannelExportItem {
	items := make([]blobChannelExportItem, 0, len(rows))
	for _, bc := range rows {
		items = append(items, blobChannelExportItem{
			BlobID: bc.BlobID, ChannelID: bc.ChannelID, State: bc.State, Attempts: bc.Attempts,
			LastErrorReason: bc.LastErrorReason, LastError: bc.LastError, UpdatedAt: bc.UpdatedAt,
		})
	}
	return items
}
