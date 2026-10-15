package adminapi

import (
	"context"
	"net/http"
	"slices"

	"github.com/x-haose/imgyoung/internal/core/channel"
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
	var (
		body map[string]any
		err  error
	)
	if p.IsAdmin() {
		body, err = h.adminDashboard(r.Context())
	} else {
		body, err = h.userDashboard(r.Context(), p.UserID)
	}
	if err != nil {
		h.dashboardFail(w, err)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, body)
}

// userDashboard 普通用户的用量概览:图片数、已用字节与配额。
// @param ctx 上下文
// @param userID 用户 id
// @return 响应体
// @return 错误
func (h *Handlers) userDashboard(ctx context.Context, userID string) (map[string]any, error) {
	images, err := h.cfg.Queries.CountImagesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	used, err := h.cfg.Queries.UserStorageUsage(ctx, userID)
	if err != nil {
		return nil, err
	}
	user, err := h.cfg.Queries.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"images":      images,
		"used_bytes":  used,
		"quota_bytes": nullableInt(user.QuotaBytes),
	}, nil
}

// adminDashboard 全站概览:计数、待办、访问与流量估算、热门图与渠道水位。
// @Description: 访问口径与计数一致(flush 周期粒度);流量按「访问数 × 原图字节」粗略估算,
// 不代表真实出站字节:304/302/变体响应同样按整张原图计入(高估),
// 同一 flush 周期内的重复访问只计一次(低估),双向误差。
// @param ctx 上下文
// @return 响应体
// @return 错误
func (h *Handlers) adminDashboard(ctx context.Context) (map[string]any, error) {
	images, err := h.cfg.Queries.CountImages(ctx)
	if err != nil {
		return nil, err
	}
	storage, err := h.cfg.Queries.TotalStorageUsage(ctx)
	if err != nil {
		return nil, err
	}
	users, err := h.cfg.Queries.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	pendingCleanup, err := h.cfg.Queries.CountPendingCleanupImages(ctx)
	if err != nil {
		return nil, err
	}
	pendingDedup, err := h.cfg.Queries.CountPendingDedupCandidates(ctx)
	if err != nil {
		return nil, err
	}
	accessTotals, err := h.cfg.Queries.AccessTotals(ctx)
	if err != nil {
		return nil, err
	}
	top, err := h.topImages(ctx)
	if err != nil {
		return nil, err
	}
	stats, noOffsite, err := h.channelOverview(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"offsite_missing":        noOffsite,
		"images":                 images,
		"storage_bytes":          storage,
		"users":                  users,
		"pending_cleanup":        pendingCleanup,
		"pending_dedup":          pendingDedup,
		"total_accesses":         accessTotals.TotalAccesses,
		"traffic_estimate_bytes": accessTotals.TrafficBytes,
		"top_images":             top,
		"channels":               stats,
	}, nil
}

// topImages 按累计访问次数降序取热门图。
// @param ctx 上下文
// @return 热门图列表,至多 topImagesLimit 条
// @return 错误
func (h *Handlers) topImages(ctx context.Context) ([]topImage, error) {
	rows, err := h.cfg.Queries.TopAccessedImages(ctx, topImagesLimit)
	if err != nil {
		return nil, err
	}
	items := make([]topImage, 0, len(rows))
	for _, row := range rows {
		items = append(items, topImage{
			ID: row.ID, URL: "/i/" + row.ID, FileName: row.FileName,
			AccessCount: row.AccessCount, Size: row.Size,
		})
	}
	return items, nil
}

// channelOverview 总览里的渠道部分:各渠道水位,以及是否缺少健康的异地渠道。
// @param ctx 上下文
// @return 渠道概览列表
// @return 是否缺少健康的异地渠道(见 offsiteMissing)
// @return 错误
func (h *Handlers) channelOverview(ctx context.Context) ([]channelStat, bool, error) {
	chans, err := h.cfg.Admin.List(ctx)
	if err != nil {
		return nil, false, err
	}
	stats, err := h.channelStats(ctx, chans)
	if err != nil {
		return nil, false, err
	}
	return stats, offsiteMissing(chans), nil
}

// offsiteMissing 判定是否缺少健康的异地渠道:健康且不在清理中的渠道里没有本机磁盘(local 驱动)以外的。
// @Description: 图片副本与元数据库快照都写到各个渠道,此时它们在本机以外没有可用的一份,主机损坏即全部丢失。
// @param chans 渠道描述列表
// @return 缺少时为 true;一个健康渠道都没有时同样为 true
func offsiteMissing(chans []channel.Info) bool {
	return !slices.ContainsFunc(chans, func(c channel.Info) bool {
		return c.Healthy && !c.Deleting && c.Driver != "local"
	})
}

// channelStats 渠道概览:每个渠道的健康状态与已存字节(副本口径)。
// @param ctx 上下文
// @param chans 渠道描述列表
// @return 渠道概览列表
// @return 错误
func (h *Handlers) channelStats(ctx context.Context, chans []channel.Info) ([]channelStat, error) {
	usage, err := h.cfg.Queries.ChannelStorageUsage(ctx)
	if err != nil {
		return nil, err
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
	return stats, nil
}

// dashboardFail 仪表盘聚合失败的统一出口。
// @param w 响应写入器
// @param err 底层错误
func (h *Handlers) dashboardFail(w http.ResponseWriter, err error) {
	h.cfg.Logger.Error("仪表盘聚合失败", "err", err)
	httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
}
