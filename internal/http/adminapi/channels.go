package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// channelBodyLimit 渠道管理请求体上限。
const channelBodyLimit = 1 << 20

// channelCreateRequest 创建渠道的请求体。
// @Description: 两种模式二选一——模板模式给 template + inputs(推荐,内置 S3 系厂商);
// 自定义模式给 driver + config(高级,直填 rclone 参数)。通用字段两种模式共用。
type channelCreateRequest struct {
	Name             string            `json:"name"`               // 渠道名(唯一)
	Priority         int64             `json:"priority"`           // 优先级,越大越优先
	Encrypted        *bool             `json:"encrypted"`          // 是否加密渠道;缺省用全局默认(创建时固化)
	AllowDirectLink  bool              `json:"allow_direct_link"`  // 是否允许 302 直链
	AutoCreateBucket bool              `json:"auto_create_bucket"` // 是否创建时主动建桶(需凭证有建桶权限)
	Template         string            `json:"template"`           // 模板模式:模板 id;空或 custom 走自定义
	Inputs           map[string]string `json:"inputs"`             // 模板模式:字段值
	Driver           string            `json:"driver"`             // 自定义模式:rclone 后端名
	Config           map[string]string `json:"config"`             // 自定义模式:后端参数(含约定键 path / public_base_url)
}

// HandleChannelTemplates 下发内置渠道模板清单(供前端渲染选模板 + 字段表单)。
func (h *Handlers) HandleChannelTemplates(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{
		Items: channel.Templates(), Total: int64(len(channel.Templates())),
	})
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
	// 模板模式:按模板合并出 driver + config;自定义模式沿用请求原值。
	driverName, config := req.Driver, req.Config
	if req.Template != "" && req.Template != "custom" {
		merged, cfg, err := channel.BuildTemplateConfig(req.Template, req.Inputs)
		if err != nil {
			httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
			return
		}
		driverName, config = merged, cfg
	}
	encrypted := h.cfg.EncryptNewChannels
	if req.Encrypted != nil {
		encrypted = *req.Encrypted
	}
	channelID, err := h.cfg.Admin.Create(r.Context(), channel.CreateParams{
		Name:             req.Name,
		Driver:           driverName,
		Config:           config,
		Priority:         req.Priority,
		Encrypted:        encrypted,
		AllowDirectLink:  req.AllowDirectLink,
		AutoCreateBucket: req.AutoCreateBucket,
	})
	switch {
	case errors.Is(err, channel.ErrInvalidChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
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

// templateErrMsg 把渠道创建错误转成面向用户的提示:去掉包前缀,透传字段必填、
// 建桶失败等已面向用户的中文消息;凭证不入错误链,展示安全。
// @param err 渠道创建返回的错误
// @return 面向用户的错误消息
func templateErrMsg(err error) string {
	return strings.TrimPrefix(err.Error(), "channel: ")
}
