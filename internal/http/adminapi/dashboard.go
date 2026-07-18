package adminapi

import (
	"net/http"

	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// channelStat 仪表盘的渠道概览项。
type channelStat struct {
	ID      int64  `json:"id"`      // 渠道 id
	Name    string `json:"name"`    // 渠道名
	Driver  string `json:"driver"`  // rclone 后端名
	Healthy bool   `json:"healthy"` // 当前健康状态
	Bytes   int64  `json:"bytes"`   // 已存字节(副本口径)
}

// topImagesLimit 仪表盘热门图列表的条数上限。
const topImagesLimit = 10

// topImage 仪表盘的热门图项(按 access_count 降序)。
type topImage struct {
	ID          string `json:"id"`           // 引用短 ID
	URL         string `json:"url"`          // 访问路径,缩略图与外链共用
	FileName    string `json:"file_name"`    // 原始文件名
	AccessCount int64  `json:"access_count"` // 累计访问次数,flush 周期粒度
	Size        int64  `json:"size"`         // 存储字节
}

// HandleDashboard 仪表盘聚合:管理员见全站,普通用户见自己的用量概览。
func (h *Handlers) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	ctx := r.Context()

	if !p.IsAdmin() {
		images, err := h.cfg.Queries.CountImagesByUser(ctx, p.UserID)
		if err != nil {
			h.dashboardFail(w, err)
			return
		}
		used, err := h.cfg.Queries.UserStorageUsage(ctx, p.UserID)
		if err != nil {
			h.dashboardFail(w, err)
			return
		}
		user, err := h.cfg.Queries.GetUser(ctx, p.UserID)
		if err != nil {
			h.dashboardFail(w, err)
			return
		}
		httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, map[string]any{
			"images":      images,
			"used_bytes":  used,
			"quota_bytes": nullableInt(user.QuotaBytes),
		})
		return
	}

	images, err := h.cfg.Queries.CountImages(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	storage, err := h.cfg.Queries.TotalStorageUsage(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	users, err := h.cfg.Queries.CountUsers(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	pendingCleanup, err := h.cfg.Queries.CountPendingCleanupImages(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	pendingDedup, err := h.cfg.Queries.CountPendingDedupCandidates(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	// 访问口径与计数一致(flush 周期粒度);流量按「访问数 × 原图字节」粗略估算,
	// 不代表真实出站字节:304/302/变体响应同样按整张原图计入(高估),
	// 同一 flush 周期内的重复访问只计一次(低估),双向误差。
	accessTotals, err := h.cfg.Queries.AccessTotals(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	topRows, err := h.cfg.Queries.TopAccessedImages(ctx, topImagesLimit)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	topImages := make([]topImage, 0, len(topRows))
	for _, row := range topRows {
		topImages = append(topImages, topImage{
			ID: row.ID, URL: "/i/" + row.ID, FileName: row.FileName,
			AccessCount: row.AccessCount, Size: row.Size,
		})
	}
	chans, err := h.cfg.Admin.List(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	usage, err := h.cfg.Queries.ChannelStorageUsage(ctx)
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	bytesByID := make(map[int64]int64, len(usage))
	for _, u := range usage {
		bytesByID[u.ID] = u.Bytes
	}
	stats := make([]channelStat, 0, len(chans))
	for _, c := range chans {
		stats = append(stats, channelStat{
			ID: c.ID, Name: c.Name, Driver: c.Driver, Healthy: c.Healthy, Bytes: bytesByID[c.ID],
		})
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, map[string]any{
		"images":                 images,
		"storage_bytes":          storage,
		"users":                  users,
		"pending_cleanup":        pendingCleanup,
		"pending_dedup":          pendingDedup,
		"total_accesses":         accessTotals.TotalAccesses,
		"traffic_estimate_bytes": accessTotals.TrafficBytes,
		"top_images":             topImages,
		"channels":               stats,
	})
}

// dashboardFail 仪表盘聚合失败的统一出口。
// @param w 响应写入器
// @param err 底层错误
func (h *Handlers) dashboardFail(w http.ResponseWriter, err error) {
	h.cfg.Logger.Error("仪表盘聚合失败", "err", err)
	httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
}
