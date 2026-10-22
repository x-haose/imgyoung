package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/importer"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// importBodyLimit 创建导入任务的请求体上限:文本模式要把文档全文带上来。
const importBodyLimit = 32 << 20

// startBodyLimit 确认导入的请求体上限:只有一组域名。
const startBodyLimit = 1 << 20

// ImportAdmin 批量导入的命令入口(由 importer.Service 实现)。
type ImportAdmin interface {
	// CreateImages 创建图片地址清单任务,任务进入 importing。
	CreateImages(ctx context.Context, opts importer.Options, urls []string) (string, error)
	// CreateText 创建文本扫描任务,扫描在调用内完成,任务进入 ready。
	CreateText(ctx context.Context, opts importer.Options, docs []importer.Document, baseURL string) (string, error)
	// CreateSite 创建站点扫描任务,任务进入 scanning。
	CreateSite(ctx context.Context, opts importer.Options, sites []string) (string, error)
	// Start 确认并开始导入所选域名下的条目;只有创建者能做。
	Start(ctx context.Context, actor importer.Actor, jobID string, hosts []string) error
	// Cancel 取消进行中或待确认的任务;管理员可以取消任何人的。
	Cancel(ctx context.Context, actor importer.Actor, jobID string) error
	// Retry 重试失败的条目并继续未处理的;只有创建者能做。
	Retry(ctx context.Context, actor importer.Actor, jobID string) error
	// Delete 删除已结束或待确认的任务;管理员可以删除任何人的。
	Delete(ctx context.Context, actor importer.Actor, jobID string) error
}

// importDocument 创建请求里的一份文本。
type importDocument struct {
	Name string `json:"name"` // 文档名
	Text string `json:"text"` // 文档全文
}

// importCreateRequest POST /imports 的请求体;各模式只能带自己的字段。
type importCreateRequest struct {
	Mode       string           `json:"mode"`        // 输入类型:images / text / site
	URLs       []string         `json:"urls"`        // images:图片地址
	Documents  []importDocument `json:"documents"`   // text:文档
	BaseURL    string           `json:"base_url"`    // text:解析相对路径的基准地址
	Sites      []string         `json:"sites"`       // site:起始地址
	TTL        json.RawMessage  `json:"ttl"`         // 导入图片的有效期,规则同 URL 转存
	EXIFPolicy string           `json:"exif_policy"` // 导入图片的 EXIF 档位
	Referer    string           `json:"referer"`     // 抓取图片时发送的 Referer
	Tags       []string         `json:"tags"`        // 导入的每张图自动带上的标签
}

// HandleImportCreate 创建导入任务。
func (h *Handlers) HandleImportCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	var req importCreateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, importBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.WriteError(h.cfg.Logger, w, http.StatusRequestEntityTooLarge, errcode.ImportInputInvalid,
				"detail", "请求体超过 32 MiB")
			return
		}
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return
	}
	opts, err := importOptions(p.UserID, req)
	if err != nil {
		h.writeImportError(w, err)
		return
	}
	jobID, err := h.createImport(r.Context(), req, opts)
	if err != nil {
		h.writeImportError(w, err)
		return
	}
	h.recordAudit(r.Context(), audit.ActionImportCreate, jobID, "mode="+req.Mode)
	row, err := h.cfg.Queries.GetImportJob(r.Context(), jobID)
	if err != nil {
		h.cfg.Logger.Error("回读导入任务失败", "job", jobID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.writeImportJob(w, r, http.StatusCreated, row)
}

// importOptions 把请求里的公共参数翻译为领域侧参数。
// @param userID 任务归属的用户 id
// @param req 请求体
// @return 公共参数
// @return 取值非法时的 *importer.InputError
func importOptions(userID string, req importCreateRequest) (importer.Options, error) {
	ttl, explicit, ok := httpx.ParseTTL(httpx.TTLString(req.TTL))
	if !ok {
		return importer.Options{}, &importer.InputError{
			Detail: "ttl 取值非法(需要 0 到 " + strconv.Itoa(httpx.MaxTTLSeconds) + " 的整数秒,或 never 表示永久)",
		}
	}
	return importer.Options{
		UserID: userID, TTL: ttl, TTLSet: explicit,
		EXIFPolicy: imaging.EXIFPolicy(req.EXIFPolicy), Referer: req.Referer, Tags: req.Tags,
	}, nil
}

// createImport 按模式创建任务,并拒绝带了其他模式字段的请求。
// @param req 请求体
// @param opts 公共参数
// @return 新任务 ID
// @return *importer.InputError 或基础设施错误
func (h *Handlers) createImport(ctx context.Context, req importCreateRequest, opts importer.Options) (string, error) {
	other := func(field string) error {
		return &importer.InputError{Detail: field + " 不属于 mode=" + req.Mode}
	}
	switch req.Mode {
	case importer.ModeImages:
		if req.Documents != nil || req.BaseURL != "" || req.Sites != nil {
			return "", other("documents、base_url 或 sites")
		}
		return h.cfg.Imports.CreateImages(ctx, opts, req.URLs)
	case importer.ModeText:
		if req.URLs != nil || req.Sites != nil {
			return "", other("urls 或 sites")
		}
		docs := make([]importer.Document, len(req.Documents))
		for i, d := range req.Documents {
			docs[i] = importer.Document{Name: d.Name, Text: d.Text}
		}
		return h.cfg.Imports.CreateText(ctx, opts, docs, req.BaseURL)
	case importer.ModeSite:
		if req.URLs != nil || req.Documents != nil || req.BaseURL != "" {
			return "", other("urls、documents 或 base_url")
		}
		return h.cfg.Imports.CreateSite(ctx, opts, req.Sites)
	default:
		return "", &importer.InputError{Detail: "mode 取值非法(可选 images / text / site)"}
	}
}

// HandleImportStart 确认并开始导入。
func (h *Handlers) HandleImportStart(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	var req struct {
		Hosts []string `json:"hosts"` // 要导入的域名
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, startBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || len(req.Hosts) == 0 {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.InvalidBody)
		return
	}
	jobID := chi.URLParam(r, "id")
	if err := h.cfg.Imports.Start(r.Context(), importActor(p), jobID, req.Hosts); err != nil {
		h.writeImportError(w, err)
		return
	}
	h.respondImport(w, r, p, jobID)
}

// HandleImportCancel 取消导入任务。
func (h *Handlers) HandleImportCancel(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	jobID := chi.URLParam(r, "id")
	row, ok := h.importJobFor(w, r, p, jobID)
	if !ok {
		return
	}
	if err := h.cfg.Imports.Cancel(r.Context(), importActor(p), jobID); err != nil {
		h.writeImportError(w, err)
		return
	}
	h.recordAudit(r.Context(), audit.ActionImportCancel, jobID, "owner="+row.UserID)
	h.respondImport(w, r, p, jobID)
}

// HandleImportRetry 重试失败的条目并继续。
func (h *Handlers) HandleImportRetry(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	jobID := chi.URLParam(r, "id")
	if err := h.cfg.Imports.Retry(r.Context(), importActor(p), jobID); err != nil {
		h.writeImportError(w, err)
		return
	}
	h.respondImport(w, r, p, jobID)
}

// HandleImportDelete 删除导入任务。
func (h *Handlers) HandleImportDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	jobID := chi.URLParam(r, "id")
	row, ok := h.importJobFor(w, r, p, jobID)
	if !ok {
		return
	}
	if err := h.cfg.Imports.Delete(r.Context(), importActor(p), jobID); err != nil {
		h.writeImportError(w, err)
		return
	}
	h.recordAudit(r.Context(), audit.ActionImportDelete, jobID, "owner="+row.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// importActor 把会话主体翻译为导入服务的操作者。
// @param p 会话主体
// @return 操作者
func importActor(p auth.Principal) importer.Actor {
	return importer.Actor{UserID: p.UserID, Admin: p.IsAdmin()}
}

// respondImport 回读任务并输出详情。任务在操作与回读之间被删除按不存在处理。
// @param p 会话主体
// @param jobID 任务 ID
func (h *Handlers) respondImport(w http.ResponseWriter, r *http.Request, p auth.Principal, jobID string) {
	row, ok := h.importJobFor(w, r, p, jobID)
	if !ok {
		return
	}
	h.writeImportJob(w, r, http.StatusOK, row)
}

// writeImportError 把导入服务的错误翻译为状态码;基础设施错误记日志、回 500。
// @param err 导入服务返回的错误
func (h *Handlers) writeImportError(w http.ResponseWriter, err error) {
	var stateErr *importer.StateError
	var inputErr *importer.InputError
	switch {
	case errors.Is(err, importer.ErrNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImportNotFound)
	case errors.Is(err, importer.ErrNotOwner):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, errcode.ImportOwnerOnly)
	case errors.As(err, &stateErr):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ImportStateInvalid)
	case errors.Is(err, importer.ErrBusy):
		w.Header().Set("Retry-After", "15")
		httpx.WriteError(h.cfg.Logger, w, http.StatusTooManyRequests, errcode.ImportBusy)
	case errors.Is(err, importer.ErrNoSelection):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.ImportNoSelection)
	case errors.Is(err, importer.ErrNothingToRetry):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ImportNothingToRetry)
	case errors.As(err, &inputErr):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.ImportInputInvalid, "detail", inputErr.Detail)
	default:
		h.cfg.Logger.Error("导入操作失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
	}
}
