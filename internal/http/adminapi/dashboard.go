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
		"images":          images,
		"storage_bytes":   storage,
		"users":           users,
		"pending_cleanup": pendingCleanup,
		"pending_dedup":   pendingDedup,
		"channels":        stats,
	})
}

// dashboardFail 仪表盘聚合失败的统一出口。
// @param w 响应写入器
// @param err 底层错误
func (h *Handlers) dashboardFail(w http.ResponseWriter, err error) {
	h.cfg.Logger.Error("仪表盘聚合失败", "err", err)
	httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
}
