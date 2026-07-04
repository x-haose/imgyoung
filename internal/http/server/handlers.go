package server

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// bearerPrefix Authorization 头的 Bearer 方案前缀。
const bearerPrefix = "Bearer "

// idempotencyHeader 客户端幂等键请求头。
const idempotencyHeader = "Idempotency-Key"

// uploadResponse 上传成功的响应体。
type uploadResponse struct {
	ID  string `json:"id"`  // 对外短 ID
	URL string `json:"url"` // 图片访问地址;配置了 base_url 时为绝对地址,否则为相对路径
}

// errorResponse API 错误的统一信封。
type errorResponse struct {
	Error string `json:"error"` // 面向用户的错误描述,不泄露内部细节
}

// requireToken 上传鉴权中间件。
// @Description: 校验 Authorization: Bearer <token>,常数时间比较防时序侧信道。
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, bearerPrefix)
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.Token)) != 1 {
			s.writeError(w, http.StatusUnauthorized, "无效的上传 Token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleHealth 健康检查端点。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte("ok")); err != nil {
		s.cfg.Logger.Debug("写健康检查响应失败", "err", err)
	}
}

// resolveExpiry 解析本次上传的到期时刻:单次 ttl 字段 > 全局默认。
// @Description: ttl 单位秒;"never" 或 0 显式永久;空则用全局默认(默认为 0 亦永久)。
// @param r 上传请求
// @return 到期时刻(零值表示永不过期)
// @return ttl 取值合法为 true;非法(非整数或负数)为 false
func (s *Server) resolveExpiry(r *http.Request) (time.Time, bool) {
	switch raw := r.FormValue("ttl"); raw {
	case "never":
		return time.Time{}, true
	case "":
		if s.cfg.DefaultTTL <= 0 {
			return time.Time{}, true
		}
		return time.Now().Add(s.cfg.DefaultTTL), true
	default:
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 0 {
			return time.Time{}, false
		}
		if secs == 0 {
			return time.Time{}, true
		}
		return time.Now().Add(time.Duration(secs) * time.Second), true
	}
}

// handleUpload 处理图片上传:HTTP 解包 → 交给上传服务 → 翻译结果。
// @Description: 业务管道(格式校验/像素预算/净化/哈希/落库)在 upload.Service;
// 本函数只负责 HTTP 层:大小闸、multipart 解析、TTL 解析、错误码映射。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, uploadErrStatus(err), "无法读取 file 字段")
		return
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			s.cfg.Logger.Debug("关闭上传文件失败", "err", cerr)
		}
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		s.writeError(w, uploadErrStatus(err), "读取上传内容失败")
		return
	}

	// 档位决策:单次表单字段 > 全局默认(在 API 层定死,Service 不做隐式回退)。
	policy := imaging.EXIFPolicy(r.FormValue("exif_policy"))
	if policy == "" {
		policy = s.cfg.DefaultEXIFPolicy
	}

	expireAt, ok := s.resolveExpiry(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "ttl 取值非法(需要非负整数秒,或 never 表示永久)")
		return
	}

	res, err := s.cfg.Upload.Process(r.Context(), data, upload.Options{
		FileName:       header.Filename,
		IdempotencyKey: r.Header.Get(idempotencyHeader),
		EXIFPolicy:     policy,
		ExpireAt:       expireAt,
	})
	switch {
	case errors.Is(err, upload.ErrUnsupportedType):
		s.writeError(w, http.StatusUnsupportedMediaType, "仅支持图片文件(jpeg/png/gif/webp)")
		return
	case errors.Is(err, upload.ErrTooManyPixels):
		s.writeError(w, http.StatusUnprocessableEntity, "图片分辨率超出限制")
		return
	case errors.Is(err, upload.ErrInvalidImage):
		s.writeError(w, http.StatusBadRequest, "图片内容无法解析")
		return
	case errors.Is(err, imaging.ErrUnknownPolicy):
		s.writeError(w, http.StatusBadRequest, "exif_policy 取值非法(可选 strip / passthrough)")
		return
	case errors.Is(err, channel.ErrNoHealthyChannel), errors.Is(err, upload.ErrInsufficientReplicas):
		s.cfg.Logger.Error("渠道不可用,上传失败", "err", err)
		s.writeError(w, http.StatusServiceUnavailable, "存储渠道暂不可用,请稍后重试")
		return
	case err != nil:
		s.cfg.Logger.Error("上传处理失败", "err", err)
		s.writeError(w, http.StatusInternalServerError, "服务内部错误")
		return
	}

	s.cfg.Logger.Info("上传成功", "id", res.ImageID, "file", header.Filename, "size", len(data))
	s.writeJSON(w, http.StatusOK, uploadResponse{
		ID:  res.ImageID,
		URL: s.cfg.BaseURL + "/i/" + res.ImageID,
	})
}

// handleServe 按短 ID 代理输出图片。
func (s *Server) handleServe(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "id")

	row, err := s.cfg.Queries.GetImageBlob(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.cfg.Logger.Error("查询引用记录失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	// 真实公开访问:记活跃度(仅内存,批量落库);后台入口不走此路径,天然不计。
	s.cfg.Activity.Touch(imageID)

	// 按需处理:解析档位参数,命中则走处理路径(即便 redirect 省流量模式也强制代理)。
	if s.cfg.Variants != nil {
		params, want, ok, varyAccept := s.parseVariantRequest(r, row.Mime)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "非法的处理参数:宽度或格式不在允许档位")
			return
		}
		if want {
			s.serveVariant(w, r, row, params, varyAccept)
			return
		}
	}
	s.serveOriginal(w, r, row, false)
}

// serveOriginal 代理或重定向原图字节:ETag/304 协商、302 省流量、按渠道优先级容灾读取。
// @param w 响应写入器
// @param r 请求
// @param row 引用与物理图信息
func (s *Server) serveOriginal(w http.ResponseWriter, r *http.Request, row sqlc.GetImageBlobRow, degraded bool) {
	imageID := row.ID
	// 非降级路径:内容不可变,以 sha256 作强 ETag;If-None-Match 命中直接 304。
	// 降级路径(处理失败回退):服务的是全尺寸原图而非目标衍生图,绝不能按 URL 强缓存/302 直链,
	// 否则会把"临时降级"当"规范内容"永久绑定到带处理参数的 URL(缓存投毒),故跳过 304/302,改 no-store。
	if !degraded {
		etag := `"` + row.Sha256 + `"`
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	candidates, err := s.cfg.Channels.CandidatesForBlob(r.Context(), row.BlobID)
	if err != nil {
		s.cfg.Logger.Error("查询渠道候选失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	if len(candidates) == 0 {
		// 引用存在但当前无健康渠道持有就绪副本;一致性由对账任务兜底。
		s.cfg.Logger.Warn("无可用渠道服务该图", "id", imageID)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}

	// 302 省流量模式:候选中允许直链且非加密的渠道直接重定向;降级路径强制代理不 302。
	if !degraded && s.cfg.RedirectMode {
		for _, cand := range candidates {
			if cand.AllowDirectLink && !cand.Encrypted && cand.PublicBaseURL != "" {
				http.Redirect(w, r, cand.PublicBaseURL+"/"+row.StorageKey, http.StatusFound)
				return
			}
		}
		// 无直链候选,回退代理。
	}

	// 访问容灾:按优先级逐个尝试,取第一个能打开的渠道。
	var rc io.ReadCloser
	for _, cand := range candidates {
		rc, err = cand.Driver.Open(r.Context(), row.StorageKey)
		if err == nil {
			break
		}
		rc = nil
		s.cfg.Logger.Warn("渠道读取失败,切换下一候选", "id", imageID, "channel", cand.Name, "err", err)
	}
	if rc == nil {
		s.cfg.Logger.Error("全部候选渠道读取失败", "id", imageID, "key", row.StorageKey)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil {
			s.cfg.Logger.Debug("关闭渠道对象失败", "id", imageID, "err", cerr)
		}
	}()

	h := w.Header()
	h.Set("Content-Type", row.Mime)
	h.Set("Content-Length", strconv.FormatInt(row.Size, 10))
	h.Set("X-Content-Type-Options", "nosniff")
	if degraded {
		// 降级响应不可被缓存,否则会污染带处理参数的 URL。
		h.Set("Cache-Control", "no-store")
	} else {
		// 内容不可变(URL 即版本),缓存一年;nosniff 强制浏览器信任服务端类型判定。
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("ETag", `"`+row.Sha256+`"`)
	}
	if _, err := io.Copy(w, rc); err != nil {
		// 多为客户端提前断开,记 debug 即可。
		s.cfg.Logger.Debug("输出图片中断", "id", imageID, "err", err)
	}
}

// uploadErrStatus 将上传读取错误映射为 HTTP 状态码。
// @Description: 超过 MaxBytesReader 上限 → 413,其余按客户端请求错误 → 400。
func uploadErrStatus(err error) int {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// writeJSON 输出 JSON 响应。
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.cfg.Logger.Debug("写 JSON 响应失败", "err", err)
	}
}

// writeError 输出统一错误信封。
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, errorResponse{Error: msg})
}
