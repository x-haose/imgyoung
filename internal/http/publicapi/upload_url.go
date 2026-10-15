package publicapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// fetchFallbackName 远端地址推不出文件名时的兜底名;文件名只作元数据展示,不参与任何逻辑。
const fetchFallbackName = "fetched"

// maxJSONBody URL 转存请求体的大小上限;正常请求只有几百字节,此闸只防喂超大 body。
const maxJSONBody = 8 << 10

// urlUploadRequest URL 转存的请求体。
type urlUploadRequest struct {
	URL        string          `json:"url"`         // 待转存的图片地址,仅 http/https
	FileName   string          `json:"file_name"`   // 覆盖由地址推断的文件名,可省
	EXIFPolicy string          `json:"exif_policy"` // 本次 EXIF 档位,省略则用全局默认
	TTL        json.RawMessage `json:"ttl"`         // 有效期秒数或 "never";省略表示未显式指定
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

// serveUploadURL URL 转存的共享管道:解析请求 → 带 SSRF 约束地抓取 → 交给上传管道。
// @Description: 抓回的字节走与直传完全相同的编排(格式白名单/像素预算/净化/哈希/配额),
// 远端声明的 Content-Type 不参与格式判定——判定一律以 magic bytes 为准。
// @param userID 归属用户 id
// @param tokenID 来源 Token 记录 id,0 表示非 Token 途径
func (h *Handlers) serveUploadURL(w http.ResponseWriter, r *http.Request, userID string, tokenID int64) {
	set := h.cfg.Settings()
	var req urlUploadRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.URLBodyInvalid)
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.URLRequired)
		return
	}

	policy := imaging.EXIFPolicy(req.EXIFPolicy)
	if policy == "" {
		policy = set.EXIFPolicy
	}
	ttl, ttlSet, ok := parseTTL(rawTTLString(req.TTL))
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadTTLInvalid,
			"max", strconv.FormatInt(maxTTLSeconds, 10))
		return
	}

	data, _, err := h.cfg.Fetch.Fetch(r.Context(), req.URL)
	if err != nil {
		h.writeFetchError(w, set, req.URL, err)
		return
	}

	name := req.FileName
	if name == "" {
		name = fileNameFromURL(req.URL)
	}
	h.runUpload(w, r, set, data, upload.Options{
		UserID:         userID,
		TokenID:        tokenID,
		FileName:       name,
		IdempotencyKey: r.Header.Get(idempotencyHeader),
		EXIFPolicy:     policy,
		TTL:            ttl,
		TTLSet:         ttlSet,
	})
}

// rawTTLString 把 JSON 里的 ttl 取值归一成字符串。
// @Description: 数字(3600)与字符串("3600" / "never")都收——客户端两种写法都自然,
// 不值得为此逼用户记住某一种。
// @param raw 原始 JSON 片段;为空表示未出现该字段
// @return 归一后的取值,空串表示未指定
func rawTTLString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	return s
}

// fileNameFromURL 从地址末段推断文件名,仅作元数据展示。
// @param rawURL 远端地址
// @return 推断出的文件名;推不出时返回兜底名
func fileNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fetchFallbackName
	}
	name := path.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return fetchFallbackName
	}
	return name
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
