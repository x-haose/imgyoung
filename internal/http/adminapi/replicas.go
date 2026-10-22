package adminapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// replicaListItem 渠道副本清单里的一行。
type replicaListItem struct {
	ImageID    *string          `json:"image_id"`    // 引用这个物理图的最早一张图片的短 ID;没有图片引用时为 null
	ImageCount int64            `json:"image_count"` // 引用这个物理图的图片张数
	State      string           `json:"state"`       // 副本状态:pending / uploading / done / failed
	Attempts   int64            `json:"attempts"`    // 累计补齐尝试次数
	Failure    *channel.Failure `json:"failure"`     // 最近一次补齐失败的结论;没有失败过为 nil
	Size       int64            `json:"size"`        // 存储字节
	Mime       string           `json:"mime"`        // 真实 MIME
	UpdatedAt  string           `json:"updated_at"`  // 状态最近更新时刻,UTC 文本
}

// replicaStates 清单过滤参数的合法取值。
var replicaStates = map[string]struct{}{"pending": {}, "failed": {}, "done": {}}

// HandleChannelReplicas 分页列出一个渠道上的副本,最近有变化的在前,可按状态过滤。
func (h *Handlers) HandleChannelReplicas(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok || !h.channelExists(w, r, channelID) {
		return
	}
	limit, offset, err := pagination(r)
	state := r.URL.Query().Get("state")
	if _, known := replicaStates[state]; err == nil && state != "" && !known {
		err = invalidParam("state")
	}
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	rows, err := h.cfg.Queries.ListChannelReplicas(r.Context(), sqlc.ListChannelReplicasParams{
		ChannelID: channelID, State: state, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		h.cfg.Logger.Error("查询渠道副本清单失败", "channel", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	total, err := h.cfg.Queries.CountChannelReplicas(r.Context(), sqlc.CountChannelReplicasParams{
		ChannelID: channelID, State: state,
	})
	if err != nil {
		h.cfg.Logger.Error("统计渠道副本失败", "channel", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	items := make([]replicaListItem, 0, len(rows))
	for _, row := range rows {
		item := replicaListItem{
			ImageCount: row.ImageCount, State: row.State, Attempts: row.Attempts, Size: row.Size, Mime: row.Mime,
			UpdatedAt: row.UpdatedAt,
		}
		if row.ImageID != "" {
			item.ImageID = &row.ImageID
		}
		if row.LastErrorReason != "" {
			item.Failure = &channel.Failure{Reason: channel.FailureReason(row.LastErrorReason), Detail: row.LastError}
		}
		items = append(items, item)
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: items, Total: total})
}

// HandleChannelReplicasRetry 把渠道上补齐失败的副本全部改回待补并重新排队。
// @Description: 清理中的渠道回 409;重新排队失败只记日志,副本已是待补,周期对账会补入队。
func (h *Handlers) HandleChannelReplicasRetry(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	ch, err := h.cfg.Queries.GetChannel(r.Context(), channelID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("读取渠道失败", "channel", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	if ch.PendingDelete != 0 {
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ChannelDeleting)
		return
	}
	n, err := h.cfg.Queries.RetryFailedBlobChannels(r.Context(), channelID)
	if err != nil {
		h.cfg.Logger.Error("重试失败的副本失败", "channel", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	if h.cfg.Replicator != nil {
		if err := h.cfg.Replicator.Reconcile(r.Context()); err != nil {
			h.cfg.Logger.Warn("重试失败的副本后重新排队未完成,留待周期对账", "channel", channelID, "err", err)
		}
	}
	h.recordAudit(r.Context(), audit.ActionChannelRetryReplicas, strconv.FormatInt(channelID, 10),
		"retried="+strconv.FormatInt(n, 10))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, struct {
		Retried int64 `json:"retried"` // 改回待补的副本数
	}{Retried: n})
}

// channelExists 确认渠道存在;不存在或查询失败时已写响应。
// @param channelID 渠道主键
// @return 存在为 true
func (h *Handlers) channelExists(w http.ResponseWriter, r *http.Request, channelID int64) bool {
	_, err := h.cfg.Queries.GetChannel(r.Context(), channelID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return false
	}
	if err != nil {
		h.cfg.Logger.Error("读取渠道失败", "channel", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return false
	}
	return true
}
