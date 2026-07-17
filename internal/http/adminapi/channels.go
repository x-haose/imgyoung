package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// channelBodyLimit 渠道管理请求体上限。
const channelBodyLimit = 1 << 20

// channelCreateRequest 创建渠道的请求体。
type channelCreateRequest struct {
	Name            string            `json:"name"`              // 渠道名(唯一)
	Driver          string            `json:"driver"`            // rclone 后端名
	Priority        int64             `json:"priority"`          // 优先级,越大越优先
	Encrypted       *bool             `json:"encrypted"`         // 是否加密渠道;缺省用全局默认(创建时固化)
	AllowDirectLink bool              `json:"allow_direct_link"` // 是否允许 302 直链
	Config          map[string]string `json:"config"`            // 后端参数(含约定键 path / public_base_url)
}

// HandleChannelCreate 创建渠道。
func (h *Handlers) HandleChannelCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, channelBodyLimit)
	dec := json.NewDecoder(r.Body)
	// 拒绝未知字段:字段名拼错静默取零值是无声的配置事故。
	dec.DisallowUnknownFields()
	var req channelCreateRequest
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体不是合法 JSON 或含未知字段")
		return
	}
	encrypted := h.cfg.EncryptNewChannels
	if req.Encrypted != nil {
		encrypted = *req.Encrypted
	}
	channelID, err := h.cfg.Admin.Create(r.Context(), channel.CreateParams{
		Name:            req.Name,
		Driver:          req.Driver,
		Config:          req.Config,
		Priority:        req.Priority,
		Encrypted:       encrypted,
		AllowDirectLink: req.AllowDirectLink,
	})
	switch {
	case errors.Is(err, channel.ErrInvalidChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "渠道参数非法或后端无法初始化")
		return
	case errors.Is(err, channel.ErrChannelNameExists):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "渠道名已存在")
		return
	case err != nil:
		h.cfg.Logger.Error("创建渠道失败", "name", req.Name, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.cfg.Logger.Info("渠道已创建", "id", channelID, "name", req.Name, "encrypted", encrypted)
	h.recordAudit(r.Context(), audit.ActionChannelCreate, req.Name,
		fmt.Sprintf("id=%d encrypted=%v", channelID, encrypted))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated, map[string]int64{"id": channelID})
}

// HandleChannelList 列出全部渠道(不含配置与凭证)。
func (h *Handlers) HandleChannelList(w http.ResponseWriter, r *http.Request) {
	list, err := h.cfg.Admin.List(r.Context())
	if err != nil {
		h.cfg.Logger.Error("查询渠道列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: list, Total: int64(len(list))})
}

// HandleChannelDelete 删除渠道;持有唯一副本时返回 409。
func (h *Handlers) HandleChannelDelete(w http.ResponseWriter, r *http.Request) {
	channelID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "渠道 ID 非法")
		return
	}
	err = h.cfg.Admin.Delete(r.Context(), channelID)
	switch {
	case errors.Is(err, channel.ErrHoldsSoleReplicas):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "该渠道持有部分图片的唯一副本,删除会丢数据;请先迁移或删除相关图片")
		return
	case err != nil:
		h.cfg.Logger.Error("删除渠道失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.cfg.Logger.Info("渠道已删除", "id", channelID)
	h.recordAudit(r.Context(), audit.ActionChannelDelete, strconv.FormatInt(channelID, 10), "")
	w.WriteHeader(http.StatusNoContent)
}
