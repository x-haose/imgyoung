package adminapi

import (
	"context"
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

// channelCreateRequest 创建渠道的请求体;测试连接接口复用同一结构。
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

// channelUpdateRequest 编辑渠道的请求体;配置部分与创建同构(模板或自定义),整体替换。
type channelUpdateRequest struct {
	Name            string            `json:"name"`              // 渠道名(唯一)
	Priority        int64             `json:"priority"`          // 优先级,越大越优先
	Encrypted       *bool             `json:"encrypted"`         // 可回传但必须与现状一致;试图变更返回 409
	AllowDirectLink bool              `json:"allow_direct_link"` // 是否允许 302 直链
	Template        string            `json:"template"`          // 模板模式:模板 id;空或 custom 走自定义
	Inputs          map[string]string `json:"inputs"`            // 模板模式:字段值
	Driver          string            `json:"driver"`            // 自定义模式:rclone 后端名
	Config          map[string]string `json:"config"`            // 自定义模式:后端参数(含约定键 path / public_base_url)
}

// channelTestRequest 测试连接的请求体:配置部分与创建同构,可携渠道 ID 支持编辑场景。
type channelTestRequest struct {
	channelCreateRequest       // 配置形态与创建一致(模板或自定义)
	ChannelID            int64 `json:"channel_id"` // 编辑场景传渠道主键,配置中的哨兵值还原为原凭证后再探测;0 = 创建场景
}

// channelTestResponse 测试连接的响应体。
type channelTestResponse struct {
	OK    bool   `json:"ok"`              // 探测是否通过
	Error string `json:"error,omitempty"` // 失败原因(人话化,不含凭证)
}

// decodeChannelBody 解码渠道管理请求体:限制体积并拒绝未知字段
// (字段名拼错静默取零值是无声的配置事故)。
// @param dst 目标结构指针
// @return 是否成功;失败时已写出 400 响应
func (h *Handlers) decodeChannelBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, channelBodyLimit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体不是合法 JSON 或含未知字段")
		return false
	}
	return true
}

// resolveChannelConfig 按请求解析出模板 id、后端名与配置:模板模式按模板合并,
// 自定义模式沿用原值。
// @param template 模板 id;空或 custom 走自定义
// @param inputs 模板字段值
// @param drv 自定义模式的后端名
// @param config 自定义模式的配置
// @return 归一化模板 id(自定义为空串)、后端名、配置、是否成功;失败时已写出 400 响应
func (h *Handlers) resolveChannelConfig(w http.ResponseWriter, template string, inputs map[string]string,
	drv string, config map[string]string) (string, string, map[string]string, bool) {
	if template == "" || template == "custom" {
		return "", drv, config, true
	}
	merged, cfg, err := channel.BuildTemplateConfig(template, inputs)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
		return "", "", nil, false
	}
	return template, merged, cfg, true
}

// channelIDParam 解析路径中的渠道 ID。
// @return 渠道 ID 与是否成功;失败时已写出 400 响应
func (h *Handlers) channelIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	channelID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "渠道 ID 非法")
		return 0, false
	}
	return channelID, true
}

// writeChannelInfo 输出指定渠道的最新完整描述(含探测结果与副本水位)。
func (h *Handlers) writeChannelInfo(w http.ResponseWriter, r *http.Request, channelID int64) {
	list, err := h.cfg.Admin.List(r.Context())
	if err != nil {
		h.cfg.Logger.Error("查询渠道描述失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	for _, c := range list {
		if c.ID == channelID {
			httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, c)
			return
		}
	}
	httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "渠道不存在")
}

// HandleChannelTemplates 下发内置渠道模板清单(供前端渲染选模板 + 字段表单)。
func (h *Handlers) HandleChannelTemplates(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{
		Items: channel.Templates(), Total: int64(len(channel.Templates())),
	})
}

// HandleChannelCreate 创建渠道:创建成功即深探测落库,并唤醒补齐管道回填存量。
func (h *Handlers) HandleChannelCreate(w http.ResponseWriter, r *http.Request) {
	var req channelCreateRequest
	if !h.decodeChannelBody(w, r, &req) {
		return
	}
	tplID, driverName, config, ok := h.resolveChannelConfig(w, req.Template, req.Inputs, req.Driver, req.Config)
	if !ok {
		return
	}
	encrypted := h.cfg.EncryptNewChannels
	if req.Encrypted != nil {
		encrypted = *req.Encrypted
	}
	channelID, err := h.cfg.Admin.Create(r.Context(), channel.CreateParams{
		Name:             req.Name,
		Driver:           driverName,
		Config:           config,
		Template:         tplID,
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
	h.kickReplication(r.Context(), req.Name)
	h.cfg.Logger.Info("渠道已创建", "id", channelID, "name", req.Name, "encrypted", encrypted)
	h.recordAudit(r.Context(), audit.ActionChannelCreate, req.Name,
		fmt.Sprintf("id=%d encrypted=%v", channelID, encrypted))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusCreated, map[string]int64{"id": channelID})
}

// kickReplication 后台唤醒补齐管道,消化新渠道随创建播种的存量 pending 行。
// @Description: 异步执行——存量大时入队耗时,不拖创建请求;唤醒失败无害,
// pending 行由定期对账兜底(围栏设计)。
// @param name 渠道名(仅日志用)
func (h *Handlers) kickReplication(ctx context.Context, name string) {
	if h.cfg.Replicator == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		if err := h.cfg.Replicator.Reconcile(bg); err != nil {
			h.cfg.Logger.Warn("新渠道补齐唤醒未完成,由定期对账兜底", "channel", name, "err", err)
		}
	}()
}

// HandleChannelUpdate 编辑渠道:配置整体替换,encrypted 不可变;成功返回最新描述。
func (h *Handlers) HandleChannelUpdate(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	var req channelUpdateRequest
	if !h.decodeChannelBody(w, r, &req) {
		return
	}
	tplID, driverName, config, ok := h.resolveChannelConfig(w, req.Template, req.Inputs, req.Driver, req.Config)
	if !ok {
		return
	}
	err := h.cfg.Admin.Update(r.Context(), channelID, channel.UpdateParams{
		Name: req.Name, Driver: driverName, Config: config, Template: tplID,
		Priority: req.Priority, Encrypted: req.Encrypted, AllowDirectLink: req.AllowDirectLink,
	})
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "渠道不存在")
		return
	case errors.Is(err, channel.ErrEncryptedImmutable):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict,
			"加密属性创建后不可变(变更会造成密文/明文混存且不可恢复);如需调整请新建渠道并迁移数据")
		return
	case errors.Is(err, channel.ErrChannelNameExists):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, "渠道名已存在")
		return
	case errors.Is(err, channel.ErrInvalidChannel):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
		return
	case err != nil:
		h.cfg.Logger.Error("编辑渠道失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.cfg.Logger.Info("渠道已编辑", "id", channelID, "name", req.Name)
	h.recordAudit(r.Context(), audit.ActionChannelUpdate, req.Name,
		fmt.Sprintf("id=%d priority=%d allow_direct_link=%v", channelID, req.Priority, req.AllowDirectLink))
	h.writeChannelInfo(w, r, channelID)
}

// HandleChannelTest 测试连接:按创建同构的请求体执行深探测,不落库、不留渠道。
// @Description: 参数形态非法(未知模板/缺必填字段/非法配置键)返回 400;
// 探测本身的成败以 {ok, error} 信封返回,供表单"先测后存"。凭证不回显;
// 编辑场景携 channel_id,配置中的哨兵值还原为该渠道原凭证后再探测。
func (h *Handlers) HandleChannelTest(w http.ResponseWriter, r *http.Request) {
	var req channelTestRequest
	if !h.decodeChannelBody(w, r, &req) {
		return
	}
	_, driverName, config, ok := h.resolveChannelConfig(w, req.Template, req.Inputs, req.Driver, req.Config)
	if !ok {
		return
	}
	if req.ChannelID > 0 {
		restored, err := h.cfg.Admin.RestoreSecrets(r.Context(), req.ChannelID, config)
		switch {
		case errors.Is(err, channel.ErrChannelNotFound):
			httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "渠道不存在")
			return
		case errors.Is(err, channel.ErrInvalidChannel):
			httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
			return
		case err != nil:
			h.cfg.Logger.Error("还原测试配置凭证失败", "id", req.ChannelID, "err", err)
			httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
			return
		}
		config = restored
	}
	err := h.cfg.Admin.TestConfig(r.Context(), driverName, config)
	if errors.Is(err, channel.ErrInvalidChannel) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
		return
	}
	resp := channelTestResponse{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, resp)
}

// HandleChannelConfig 回读单渠道配置的编辑视图(敏感值哨兵掩码,供编辑表单预填)。
// @Description: 提交时把哨兵值原样回传即表示保留原凭证,配合 PUT /channels/{id}
// 完成不重录凭证的编辑;凭证明文永不下发。
func (h *Handlers) HandleChannelConfig(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	cfg, err := h.cfg.Admin.ConfigForEdit(r.Context(), channelID)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "渠道不存在")
		return
	case err != nil:
		h.cfg.Logger.Error("回读渠道配置失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, cfg)
}

// HandleChannelCheck 手动重检:对已有渠道执行深探测并返回探测后的完整描述。
func (h *Handlers) HandleChannelCheck(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	err := h.cfg.Admin.ProbeChannel(r.Context(), channelID)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "渠道不存在")
		return
	case err != nil:
		h.cfg.Logger.Error("手动重检失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.writeChannelInfo(w, r, channelID)
}

// HandleChannelList 列出全部渠道(不含配置与凭证,含探测结果与副本水位)。
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
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	err := h.cfg.Admin.Delete(r.Context(), channelID)
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

// templateErrMsg 把渠道操作错误转成面向用户的提示:去掉包前缀,透传字段必填、
// 建桶失败等已面向用户的中文消息;凭证不入错误链,展示安全。
// @param err 渠道操作返回的错误
// @return 面向用户的错误消息
func templateErrMsg(err error) string {
	return strings.TrimPrefix(err.Error(), "channel: ")
}
