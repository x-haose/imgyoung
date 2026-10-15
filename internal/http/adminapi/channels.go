package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// channelBodyLimit 渠道管理请求体上限。
const channelBodyLimit = 1 << 20

// channelSource 渠道配置的来源,两种模式二选一:模板模式给 template + inputs(内置厂商模板),
// 自定义模式给 driver + config(直填 rclone 参数)。创建、编辑与测试连接的请求体共用。
type channelSource struct {
	Template string            `json:"template"` // 模板模式:模板 id;空或 custom 走自定义
	Inputs   map[string]string `json:"inputs"`   // 模板模式:字段值
	Driver   string            `json:"driver"`   // 自定义模式:rclone 后端名
	Config   map[string]string `json:"config"`   // 自定义模式:后端参数(含约定键 path / public_base_url)
}

// channelSpec 归一化后的渠道配置:模板模式按模板合并出后端参数,自定义模式沿用原值。
type channelSpec struct {
	template string            // 归一化模板 id;自定义为空串
	driver   string            // rclone 后端名
	config   map[string]string // 后端参数
}

// channelCreateRequest 创建渠道的请求体;测试连接接口复用同一结构。
type channelCreateRequest struct {
	channelSource           // 配置来源(模板或自定义)
	Name             string `json:"name"`               // 渠道名(唯一)
	Priority         int64  `json:"priority"`           // 优先级,越大越优先
	Encrypted        *bool  `json:"encrypted"`          // 是否加密渠道;缺省用全局默认(创建时固化)
	AllowDirectLink  bool   `json:"allow_direct_link"`  // 是否允许 302 直链
	AutoCreateBucket bool   `json:"auto_create_bucket"` // 是否创建时主动建桶(需凭证有建桶权限)
}

// channelUpdateRequest 编辑渠道的请求体;配置部分与创建同构,整体替换。
type channelUpdateRequest struct {
	channelSource          // 配置来源(模板或自定义)
	Name            string `json:"name"`              // 渠道名(唯一)
	Priority        int64  `json:"priority"`          // 优先级,越大越优先
	Encrypted       *bool  `json:"encrypted"`         // 可回传但必须与现状一致;试图变更返回 409
	AllowDirectLink bool   `json:"allow_direct_link"` // 是否允许 302 直链
}

// channelTestRequest 测试连接的请求体:配置部分与创建同构,可携渠道 ID 支持编辑场景。
type channelTestRequest struct {
	channelCreateRequest       // 配置形态与创建一致(模板或自定义)
	ChannelID            int64 `json:"channel_id"` // 编辑场景传渠道主键,配置中的哨兵值还原为原凭证后再探测;0 = 创建场景
}

// channelTestResponse 测试连接的响应体。
type channelTestResponse struct {
	OK      bool             `json:"ok"`                // 探测是否通过
	Failure *channel.Failure `json:"failure,omitempty"` // 探测失败的结论,原文不含凭证;通过时省略
	Token   string           `json:"token,omitempty"`   // 测试期间服务商刷新了提交的授权令牌时为新令牌 JSON;前端须用它替换表单里的令牌再保存
}

// channelSaveError 创建、编辑渠道失败时的响应体:错误信封,外加保存过程中服务商刷新出的令牌。
type channelSaveError struct {
	httpx.ErrorResponse        // 失败原因
	Token               string `json:"token,omitempty"` // 服务商刷新了表单提交的令牌时为新令牌 JSON,表单须换用它再提交;原令牌可能已作废
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
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return false
	}
	return true
}

// resolveChannelConfig 把请求里的配置来源归一化:模板模式按模板合并出后端参数,自定义模式沿用原值。
// @param w 响应写入器
// @param src 请求携带的配置来源
// @return 归一化配置
// @return 是否成功;失败时已写出 400 响应
func (h *Handlers) resolveChannelConfig(w http.ResponseWriter, src channelSource) (channelSpec, bool) {
	if src.Template == "" || src.Template == "custom" {
		return channelSpec{driver: src.Driver, config: src.Config}, true
	}
	driverName, cfg, err := channel.BuildTemplateConfig(src.Template, src.Inputs)
	if err != nil {
		httpx.WriteErrorFrom(h.cfg.Logger, w, http.StatusBadRequest, err)
		return channelSpec{}, false
	}
	return channelSpec{template: src.Template, driver: driverName, config: cfg}, true
}

// channelIDParam 解析路径中的渠道 ID。
// @return 渠道 ID 与是否成功;失败时已写出 400 响应
func (h *Handlers) channelIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	channelID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.ChannelIDInvalid)
		return 0, false
	}
	return channelID, true
}

// writeChannelInfo 输出指定渠道的最新完整描述(含探测结果与副本水位)。
// @param status 成功时的响应状态码
func (h *Handlers) writeChannelInfo(w http.ResponseWriter, r *http.Request, channelID int64, status int) {
	list, err := h.cfg.Admin.List(r.Context())
	if err != nil {
		h.cfg.Logger.Error("查询渠道描述失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	for _, c := range list {
		if c.ID == channelID {
			httpx.WriteJSON(h.cfg.Logger, w, status, c)
			return
		}
	}
	httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
}

// HandleChannelTemplates 下发内置渠道模板清单(供前端渲染选模板 + 字段表单)。
func (h *Handlers) HandleChannelTemplates(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{
		Items: channel.Templates(), Total: int64(len(channel.Templates())),
	})
}

// HandleChannelCreate 创建渠道:创建成功即深探测落库,并唤醒补齐管道回填存量;成功返回 201 与完整描述。
func (h *Handlers) HandleChannelCreate(w http.ResponseWriter, r *http.Request) {
	var req channelCreateRequest
	if !h.decodeChannelBody(w, r, &req) {
		return
	}
	spec, ok := h.resolveChannelConfig(w, req.channelSource)
	if !ok {
		return
	}
	encrypted := h.cfg.Settings.Current().EncryptNewChannels
	if req.Encrypted != nil {
		encrypted = *req.Encrypted
	}
	channelID, err := h.cfg.Admin.Create(r.Context(), channel.CreateParams{
		Name:             req.Name,
		Driver:           spec.driver,
		Config:           spec.config,
		Template:         spec.template,
		Priority:         req.Priority,
		Encrypted:        encrypted,
		AllowDirectLink:  req.AllowDirectLink,
		AutoCreateBucket: req.AutoCreateBucket,
	})
	if err != nil {
		h.writeChannelSaveError(w, err, "创建渠道失败", "name", req.Name)
		return
	}
	h.kickReplication(r.Context(), req.Name)
	h.cfg.Logger.Info("渠道已创建", "id", channelID, "name", req.Name, "encrypted", encrypted)
	h.recordAudit(r.Context(), audit.ActionChannelCreate, strconv.FormatInt(channelID, 10),
		fmt.Sprintf("name=%s encrypted=%v", req.Name, encrypted))
	h.writeChannelInfo(w, r, channelID, http.StatusCreated)
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
	spec, ok := h.resolveChannelConfig(w, req.channelSource)
	if !ok {
		return
	}
	err := h.cfg.Admin.Update(r.Context(), channelID, channel.UpdateParams{
		Name: req.Name, Driver: spec.driver, Config: spec.config, Template: spec.template,
		Priority: req.Priority, Encrypted: req.Encrypted, AllowDirectLink: req.AllowDirectLink,
	})
	if err != nil {
		h.writeChannelSaveError(w, err, "编辑渠道失败", "id", channelID)
		return
	}
	h.cfg.Logger.Info("渠道已编辑", "id", channelID, "name", req.Name)
	h.recordAudit(r.Context(), audit.ActionChannelUpdate, strconv.FormatInt(channelID, 10),
		fmt.Sprintf("name=%s priority=%d allow_direct_link=%v", req.Name, req.Priority, req.AllowDirectLink))
	h.writeChannelInfo(w, r, channelID, http.StatusOK)
}

// writeChannelSaveError 把创建、编辑渠道的失败写成响应;保存过程中服务商刷新出的令牌随错误交还。
// @param err 创建或编辑返回的错误
// @param logMsg 服务端故障时的日志消息
// @param logArgs 日志字段,键值交替
func (h *Handlers) writeChannelSaveError(w http.ResponseWriter, err error, logMsg string, logArgs ...any) {
	var body channelSaveError
	if tr, ok := errors.AsType[*channel.TokenRefreshedError](err); ok {
		body.Token = tr.Token
	}
	status, e := http.StatusInternalServerError, errcode.New(errcode.Internal)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		status, e = http.StatusNotFound, errcode.New(errcode.ChannelNotFound)
	case errors.Is(err, channel.ErrChannelDeleting):
		status, e = http.StatusConflict, errcode.New(errcode.ChannelDeleting)
	case errors.Is(err, channel.ErrChannelConfigChanged):
		status, e = http.StatusConflict, errcode.New(errcode.ChannelConfigChanged)
	case errors.Is(err, channel.ErrEncryptedImmutable):
		status, e = http.StatusConflict, errcode.New(errcode.ChannelEncryptedImmutable)
	case errors.Is(err, channel.ErrChannelNameExists):
		status, e = http.StatusConflict, errcode.New(errcode.ChannelNameExists)
	default:
		if coded, ok := errcode.From(err); ok && errors.Is(err, channel.ErrInvalidChannel) {
			status, e = http.StatusBadRequest, coded
		} else {
			h.cfg.Logger.Error(logMsg, append(logArgs, "err", err)...)
		}
	}
	body.ErrorResponse = httpx.ErrorBody(e)
	httpx.WriteJSON(h.cfg.Logger, w, status, body)
}

// HandleChannelTest 测试连接:按创建同构的请求体执行深探测,不落库、不留渠道。
// @Description: 参数形态非法(未知模板/缺必填字段/非法配置键)返回 400;
// 探测本身的成败以 {ok, failure} 返回,供表单"先测后存"。凭证不回显,唯一例外是服务商刷新了
// 提交的授权令牌:新令牌随 token 返回,表单换用它保存,原令牌可能已被服务商作废。
// 编辑场景携 channel_id,配置中的哨兵值还原为该渠道原凭证后再探测;清理中的渠道返回 409。
func (h *Handlers) HandleChannelTest(w http.ResponseWriter, r *http.Request) {
	var req channelTestRequest
	if !h.decodeChannelBody(w, r, &req) {
		return
	}
	spec, ok := h.resolveChannelConfig(w, req.channelSource)
	if !ok {
		return
	}
	res, err := h.cfg.Admin.TestConfig(r.Context(), channel.TestParams{
		ChannelID: req.ChannelID, Template: spec.template, Driver: spec.driver, Config: spec.config,
	})
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	case errors.Is(err, channel.ErrChannelDeleting):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ChannelDeleting)
		return
	case errors.Is(err, channel.ErrInvalidChannel):
		httpx.WriteErrorFrom(h.cfg.Logger, w, http.StatusBadRequest, err)
		return
	case err != nil:
		h.cfg.Logger.Error("测试连接失败", "id", req.ChannelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, channelTestResponse{
		OK: res.Failure == nil, Failure: res.Failure, Token: res.Token,
	})
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
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	case err != nil:
		h.cfg.Logger.Error("回读渠道配置失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, cfg)
}

// HandleChannelRcloneConfig 以附件下载渠道的 rclone 配置文件,标准 rclone 凭它即可脱离本服务读写渠道数据。
// @Description: 文件含渠道凭证与加密渠道的解密密码:响应不进任何缓存,每次下载记一条审计。
// 文件头注释的语言取查询参数 lang(管理后台带上界面语言),缺省按 Accept-Language。配置里有 rclone 配置文件
// 无法表达的值时返回 422,原因只指出远端与参数名。
func (h *Handlers) HandleChannelRcloneConfig(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	lang, ok := exportLanguage(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.LangInvalid)
		return
	}
	file, err := h.cfg.Admin.RcloneConfig(r.Context(), channelID, lang)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	case errors.Is(err, channel.ErrInvalidChannel):
		httpx.WriteErrorFrom(h.cfg.Logger, w, http.StatusUnprocessableEntity, err)
		return
	case err != nil:
		h.cfg.Logger.Error("生成渠道 rclone 配置失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionChannelExport, strconv.FormatInt(channelID, 10), "rclone-config")
	hd := w.Header()
	hd.Set("Content-Type", "text/plain; charset=utf-8")
	hd.Set("Content-Length", strconv.Itoa(len(file.Content)))
	hd.Set("Content-Disposition", `attachment; filename="`+file.Name+`"`)
	hd.Set("Content-Language", string(lang))
	hd.Set("Vary", "Accept-Language")
	hd.Set("Cache-Control", "no-store")
	// 与快照下载一致以流式拷贝输出附件;显式纯文本类型 + 全局 nosniff,浏览器不会把它当页面渲染。
	if _, err := io.Copy(w, strings.NewReader(file.Content)); err != nil {
		h.cfg.Logger.Debug("输出渠道 rclone 配置中断", "id", channelID, "err", err)
	}
}

// exportLanguage 取导出文件说明文字的语言:查询参数 lang 优先,缺省按 Accept-Language。
// @param r 当前请求
// @return 语言
// @return lang 取值不是支持的语言时为 false
func exportLanguage(r *http.Request) (channel.Language, bool) {
	switch lang := channel.Language(r.URL.Query().Get("lang")); lang {
	case channel.LanguageChinese, channel.LanguageEnglish:
		return lang, true
	case "":
		if httpx.PrefersChinese(r) {
			return channel.LanguageChinese, true
		}
		return channel.LanguageEnglish, true
	default:
		return "", false
	}
}

// HandleChannelCheck 手动重检:对已有渠道执行深探测并返回探测后的完整描述。
// @Description: 探测可能持续数十秒;客户端中途断开时探测结论不落库,此处也无人可答,直接返回。
// 清理中的渠道不探测,返回 409。
func (h *Handlers) HandleChannelCheck(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	err := h.cfg.Admin.ProbeChannel(r.Context(), channelID)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	case errors.Is(err, channel.ErrChannelDeleting):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ChannelDeleting)
		return
	case r.Context().Err() != nil:
		return
	case err != nil:
		h.cfg.Logger.Error("手动重检失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.writeChannelInfo(w, r, channelID, http.StatusOK)
}

// HandleChannelList 列出全部渠道(不含配置与凭证,含探测结果与副本水位)。
func (h *Handlers) HandleChannelList(w http.ResponseWriter, r *http.Request) {
	list, err := h.cfg.Admin.List(r.Context())
	if err != nil {
		h.cfg.Logger.Error("查询渠道列表失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, listResponse{Items: list, Total: int64(len(list))})
}

// HandleChannelDelete 删除渠道;持有唯一副本时返回 409。
// @Description: 查询参数 purge(缺省 true)决定是否清理本服务写在渠道上的对象(图片副本、系统目录下的
// 快照等与深探测残留):清理时只打清理中标记、入队排空任务并返回 202,渠道在列表里停留为"清理中"直到
// 对象删完;purge=false 立即删行返回 204,远端对象原样保留。排空入队失败只告警——标记已落库,定期对账会补入队。
func (h *Handlers) HandleChannelDelete(w http.ResponseWriter, r *http.Request) {
	channelID, ok := h.channelIDParam(w, r)
	if !ok {
		return
	}
	purge := true
	if raw := r.URL.Query().Get("purge"); raw != "" {
		parsed, perr := strconv.ParseBool(raw)
		if perr != nil {
			httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.PurgeParamInvalid)
			return
		}
		purge = parsed
	}
	if purge && h.cfg.Replicator == nil {
		h.cfg.Logger.Error("删除渠道要求清理对象,但未装配补齐子系统", "id", channelID)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	err := h.cfg.Admin.Delete(r.Context(), channelID, purge)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	case errors.Is(err, channel.ErrHoldsSoleReplicas):
		httpx.WriteErrorFrom(h.cfg.Logger, w, http.StatusConflict, err)
		return
	case err != nil:
		h.cfg.Logger.Error("删除渠道失败", "id", channelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.recordAudit(r.Context(), audit.ActionChannelDelete, strconv.FormatInt(channelID, 10),
		fmt.Sprintf("purge=%v", purge))
	if !purge {
		h.cfg.Logger.Info("渠道已删除,远端对象保留", "id", channelID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.cfg.Replicator.EnqueueChannelDrain(r.Context(), channelID); err != nil {
		h.cfg.Logger.Warn("渠道排空任务入队失败,由定期对账补入队", "id", channelID, "err", err)
	}
	h.cfg.Logger.Info("渠道已标记清理中,对象删完后移除", "id", channelID)
	w.WriteHeader(http.StatusAccepted)
}
