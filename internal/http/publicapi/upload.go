package publicapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// idempotencyHeader 客户端幂等键请求头。
const idempotencyHeader = "Idempotency-Key"

// uploadResponse 上传成功的响应体。
type uploadResponse struct {
	ID  string `json:"id"`  // 对外短 ID
	URL string `json:"url"` // 图片访问地址;配置了 base_url 时为绝对地址,否则为相对路径
}

// HandleHealth 健康检查端点。
func (h *Handlers) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte("ok")); err != nil {
		h.cfg.Logger.Debug("写健康检查响应失败", "err", err)
	}
}

// resolveExpiry 解析本次上传的到期时刻:单次 ttl 字段 > 全局默认。
// @Description: ttl 单位秒;"never" 或 0 显式永久;空则用全局默认(默认为 0 亦永久)。
// @param r 上传请求
// @return 到期时刻(零值表示永不过期)
// @return ttl 取值合法为 true;非法(非整数或负数)为 false
func (h *Handlers) resolveExpiry(r *http.Request) (time.Time, bool) {
	switch raw := r.FormValue("ttl"); raw {
	case "never":
		return time.Time{}, true
	case "":
		if h.cfg.DefaultTTL <= 0 {
			return time.Time{}, true
		}
		return time.Now().Add(h.cfg.DefaultTTL), true
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

// HandleUpload 处理图片上传:HTTP 解包 → 交给上传服务 → 翻译结果。
// @Description: 业务管道(格式校验/像素预算/净化/哈希/落库)在 upload.Service;
// 本函数只负责 HTTP 层:大小闸、multipart 解析、TTL 解析、错误码映射;
// 归属用户与来源 Token 由认证中间件解析、此处显式传参进 core。
func (h *Handlers) HandleUpload(w http.ResponseWriter, r *http.Request) {
	tp, ok := middleware.TokenPrincipalFrom(r.Context())
	if !ok {
		// 路由保证本 handler 挂在 Token 认证之后,取不到主体属装配错误。
		h.cfg.Logger.Error("上传请求缺少认证主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, uploadErrStatus(err), "无法读取 file 字段")
		return
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			h.cfg.Logger.Debug("关闭上传文件失败", "err", cerr)
		}
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, uploadErrStatus(err), "读取上传内容失败")
		return
	}

	// 档位决策:单次表单字段 > 全局默认(在 API 层定死,Service 不做隐式回退)。
	policy := imaging.EXIFPolicy(r.FormValue("exif_policy"))
	if policy == "" {
		policy = h.cfg.DefaultEXIFPolicy
	}

	expireAt, ok := h.resolveExpiry(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "ttl 取值非法(需要非负整数秒,或 never 表示永久)")
		return
	}

	res, err := h.cfg.Upload.Process(r.Context(), data, upload.Options{
		UserID:         tp.UserID,
		TokenID:        tp.TokenID,
		FileName:       header.Filename,
		IdempotencyKey: r.Header.Get(idempotencyHeader),
		EXIFPolicy:     policy,
		ExpireAt:       expireAt,
	})
	switch {
	case errors.Is(err, upload.ErrUnsupportedType):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnsupportedMediaType, "仅支持图片文件(jpeg/png/gif/webp)")
		return
	case errors.Is(err, upload.ErrTooManyPixels):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnprocessableEntity, "图片分辨率超出限制")
		return
	case errors.Is(err, upload.ErrInvalidImage):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "图片内容无法解析")
		return
	case errors.Is(err, imaging.ErrUnknownPolicy):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "exif_policy 取值非法(可选 strip / passthrough)")
		return
	case errors.Is(err, upload.ErrQuotaExceeded):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, "存储配额已满")
		return
	case errors.Is(err, channel.ErrNoHealthyChannel), errors.Is(err, upload.ErrInsufficientReplicas):
		h.cfg.Logger.Error("渠道不可用,上传失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, "存储渠道暂不可用,请稍后重试")
		return
	case err != nil:
		h.cfg.Logger.Error("上传处理失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}

	h.cfg.Logger.Info("上传成功", "id", res.ImageID, "file", header.Filename, "size", len(data))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, uploadResponse{
		ID:  res.ImageID,
		URL: h.cfg.BaseURL + "/i/" + res.ImageID,
	})
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
