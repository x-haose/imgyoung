package adminapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"strconv"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/recovery"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// recoveryBodyLimit 找回请求体上限。
const recoveryBodyLimit = 1 << 10

// HandleImageRecovery 按渠道里的对象找回缺少记录的图片(仅管理员路由)。
// @Description: 请求体可省略,省略时归属当前管理员;请求断开即中止,已登记的保留,可重复调用。
// 已有一轮在进行回 409,没有健康渠道回 503,指定的用户不存在回 404;其余失败回 500。
func (h *Handlers) HandleImageRecovery(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, recoveryBodyLimit)
	var req struct {
		UserID string `json:"user_id"` // 找回的图片归属的用户 id;空表示当前管理员
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return
	}
	if req.UserID == "" {
		req.UserID = principal.UserID
	}
	res, err := h.cfg.Recovery.Recover(r.Context(), req.UserID)
	if err != nil {
		h.writeRecoveryError(w, r, res, err)
		return
	}
	h.recordAudit(r.Context(), audit.ActionImageRecover, req.UserID,
		"recovered="+strconv.Itoa(res.Recovered)+" failed="+strconv.Itoa(res.Failed))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, res)
}

// writeRecoveryError 把找回的错误翻译为状态码;请求已断开时只记日志,无人接收响应。
// @param w 响应写入器
// @param r 请求
// @param res 找回中止时已完成部分的统计
// @param err Recover 返回的错误
func (h *Handlers) writeRecoveryError(w http.ResponseWriter, r *http.Request, res recovery.Result, err error) {
	switch {
	case errors.Is(err, recovery.ErrBusy):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.RecoveryBusy)
	case errors.Is(err, recovery.ErrUserNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.UserNotFound)
	case errors.Is(err, channel.ErrNoHealthyChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, errcode.NoHealthyChannel)
	case r.Context().Err() != nil:
		h.cfg.Logger.Info("发起找回的请求已断开,找回中止", "recovered", res.Recovered, "err", err)
	default:
		h.cfg.Logger.Error("找回图片记录失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
	}
}
