package publicapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// maxJSONBody URL 转存请求体的大小上限;正常请求只有几百字节,此闸只防喂超大 body。
const maxJSONBody = 8 << 10

// urlUploadRequest URL 转存的请求体。
type urlUploadRequest struct {
	URL        string          `json:"url"`         // 待转存的图片地址,仅 http/https
	FileName   string          `json:"file_name"`   // 覆盖由地址推断的文件名,可省
	EXIFPolicy string          `json:"exif_policy"` // 本次 EXIF 档位,省略则用全局默认
	TTL        json.RawMessage `json:"ttl"`         // 有效期秒数或 "never";省略表示未显式指定
	Private    bool            `json:"private"`     // 是否作为私有图保存
	SignedTTL  json.RawMessage `json:"signed_ttl"`  // 私有图响应里签名链接的有效秒数,省略为 24 小时
}

// HandleUploadURL 处理数据面 URL 转存(Bearer Token 认证)。
func (h *Handlers) HandleUploadURL(w http.ResponseWriter, r *http.Request) {
	tp, ok := middleware.TokenPrincipalFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("URL 转存请求缺少 Token 主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.serveUploadURL(w, r, tp.UserID, tp.TokenID)
}

// HandleSessionUploadURL 处理管理后台 URL 转存(会话认证)。
func (h *Handlers) HandleSessionUploadURL(w http.ResponseWriter, r *http.Request) {
	si, ok := middleware.SessionFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("后台 URL 转存请求缺少会话主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	h.serveUploadURL(w, r, si.Principal.UserID, 0)
}

// serveUploadURL URL 转存的共享管道:解析并校验请求 → 查幂等键 → 带 SSRF 约束地抓取 → 交给上传管道。
// @Description: 幂等键在参数校验之后、抓取之前查(与表单上传同序),命中即返回第一次的结果,不再访问远端。
// 抓回的字节走与直传完全相同的编排(格式白名单/像素预算/净化/哈希/配额),远端声明的 Content-Type
// 不参与格式判定——判定一律以 magic bytes 为准。
// @param userID 归属用户 id
// @param tokenID 来源 Token 记录 id,0 表示非 Token 途径
func (h *Handlers) serveUploadURL(w http.ResponseWriter, r *http.Request, userID string, tokenID int64) {
	set := h.cfg.Settings()
	req, ok := h.parseURLUpload(w, r, set)
	if !ok {
		return
	}
	idemKey := r.Header.Get(idempotencyHeader)
	if h.respondExisting(w, r, set, userID, idemKey, req.priv.signedTTL) {
		return
	}

	data, _, err := h.cfg.Fetch.Fetch(r.Context(), req.url)
	if err != nil {
		h.writeFetchError(w, set, req.url, err)
		return
	}
	h.runUpload(w, r, set, data, upload.Options{
		UserID:         userID,
		TokenID:        tokenID,
		FileName:       req.fileName,
		IdempotencyKey: idemKey,
		EXIFPolicy:     req.policy,
		TTL:            req.ttl,
		TTLSet:         req.ttlSet,
		Private:        req.priv.private,
	}, req.priv.signedTTL)
}

// urlUpload 解析并校验过的 URL 转存参数。
type urlUpload struct {
	url      string             // 待转存的远端地址
	fileName string             // 文件名:请求显式给出的,否则由地址末段推断
	policy   imaging.EXIFPolicy // 本次生效的 EXIF 档位,已校验合法
	ttl      time.Duration      // 显式指定的有效时长,仅 ttlSet 为 true 时生效;0 表示显式永久
	ttlSet   bool               // 请求是否显式指定了 ttl
	priv     privacy            // 私有选项
}

// parseURLUpload 解析 URL 转存的请求体并校验参数。
// @Description: 校验顺序与表单上传一致:先 ttl,再 EXIF 档位(省略时取全局默认);校验失败时已写出错误响应。
// @param set 配置快照
// @return 解析后的参数
// @return 是否通过校验
func (h *Handlers) parseURLUpload(w http.ResponseWriter, r *http.Request, set config.Settings) (urlUpload, bool) {
	var req urlUploadRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.URLBodyInvalid)
		return urlUpload{}, false
	}
	if strings.TrimSpace(req.URL) == "" {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.URLRequired)
		return urlUpload{}, false
	}
	ttl, ttlSet, ok := httpx.ParseTTL(httpx.TTLString(req.TTL))
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadTTLInvalid,
			"max", strconv.FormatInt(httpx.MaxTTLSeconds, 10))
		return urlUpload{}, false
	}
	policy := imaging.EXIFPolicy(req.EXIFPolicy)
	if policy == "" {
		policy = set.EXIFPolicy
	}
	if err := imaging.ValidatePolicy(policy); err != nil {
		h.writeUploadError(w, set, err)
		return urlUpload{}, false
	}
	priv, ok := parsePrivacy(strconv.FormatBool(req.Private), httpx.TTLString(req.SignedTTL))
	if !ok {
		h.writePrivacyInvalid(w)
		return urlUpload{}, false
	}
	name := req.FileName
	if name == "" {
		name = urlfetch.FileName(req.URL)
	}
	return urlUpload{url: req.URL, fileName: name, policy: policy, ttl: ttl, ttlSet: ttlSet, priv: priv}, true
}

// respondExisting 幂等键命中时直接返回第一次的结果。
// @param set 配置快照
// @param userID 归属用户 id
// @param key 幂等键,可为空(视为未命中)
// @param signedTTL 命中的是私有图时,响应里签名链接的有效时长
// @return 是否已写出响应:命中,或查询失败已写出错误
func (h *Handlers) respondExisting(w http.ResponseWriter, r *http.Request, set config.Settings,
	userID, key string, signedTTL time.Duration) bool {
	imageID, found, err := h.cfg.Upload.ExistingUpload(r.Context(), userID, key)
	if err != nil {
		h.writeUploadError(w, set, err)
		return true
	}
	if found {
		h.writeUploaded(w, r, set, imageID, signedTTL)
	}
	return found
}

// writeFetchError 把抓取失败映射为状态码与可读理由。
// @Description: SSRF 拒绝与远端故障都记日志带上原始地址——这类失败的排查全靠它,
// 但对外只回归因,不回显远端响应内容。
// @param set 配置快照,大小上限文案与闸值同源
// @param rawURL 请求转存的地址
// @param err 抓取阶段的原始错误
func (h *Handlers) writeFetchError(w http.ResponseWriter, set config.Settings, rawURL string, err error) {
	switch {
	case errors.Is(err, urlfetch.ErrScheme):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.URLScheme)
	case errors.Is(err, urlfetch.ErrBlockedAddress):
		h.cfg.Logger.Warn("URL 转存目标被地址策略拒绝", "url", rawURL)
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.URLBlocked)
	case errors.Is(err, urlfetch.ErrTooManyRedirects):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadGateway, errcode.URLTooManyRedirects)
	case errors.Is(err, urlfetch.ErrTooLarge):
		httpx.WriteError(h.cfg.Logger, w, http.StatusRequestEntityTooLarge, errcode.URLTooLarge,
			"max", strconv.FormatInt(set.MaxUploadMB, 10))
	case errors.Is(err, urlfetch.ErrStatus):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadGateway, errcode.URLBadStatus)
	default:
		h.cfg.Logger.Warn("URL 转存抓取失败", "url", rawURL, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadGateway, errcode.URLFetchFailed)
	}
}
