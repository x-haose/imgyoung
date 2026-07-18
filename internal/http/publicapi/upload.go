package publicapi

import (
	"errors"
	"fmt"
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

// maxTTLSeconds ttl 秒数上限(100 年),与管理面 ttl_hours 上限同口径;
// 同时挡住 time.Duration 乘法溢出(溢出为负会被误判为永久)。
const maxTTLSeconds = 876000 * 3600

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

// resolveTTL 解析本次上传的有效时长:单次 ttl 字段 > 全局默认。
// @Description: ttl 单位秒;"never" 或 0 显式永久;空则用全局默认(默认为 0 亦永久)。
// @param r 上传请求
// @return 有效时长(0 表示永不过期)
// @return ttl 取值合法为 true;非法(非整数、负数或超上限)为 false
func (h *Handlers) resolveTTL(r *http.Request) (time.Duration, bool) {
	switch raw := r.FormValue("ttl"); raw {
	case "never":
		return 0, true
	case "":
		if h.cfg.DefaultTTL <= 0 {
			return 0, true
		}
		return h.cfg.DefaultTTL, true
	default:
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 0 || secs > maxTTLSeconds {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
}

// HandleUpload 处理数据面图片上传(Bearer Token 认证)。
// @Description: 归属用户与来源 Token 由 TokenAuth 中间件解析,此处取出后
// 交给共享上传管道;取不到主体属路由装配错误。
func (h *Handlers) HandleUpload(w http.ResponseWriter, r *http.Request) {
	tp, ok := middleware.TokenPrincipalFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("上传请求缺少 Token 主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.serveUpload(w, r, tp.UserID, tp.TokenID)
}

// HandleSessionUpload 处理管理后台图片上传(会话认证)。
// @Description: 与数据面走完全相同的上传管道与错误映射;归属用户取会话主体,
// token_id 记空(非 Token 途径)。挂在 SessionAuth+CSRF 之后,取不到会话属路由装配错误。
func (h *Handlers) HandleSessionUpload(w http.ResponseWriter, r *http.Request) {
	si, ok := middleware.SessionFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("后台上传请求缺少会话主体,检查路由装配")
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	h.serveUpload(w, r, si.Principal.UserID, 0)
}

// serveUpload 上传的共享 HTTP 管道:大小闸、multipart 解析、档位与 TTL 解析、
// 调用上传服务并写响应;业务管道(格式校验/像素预算/净化/哈希/落库)在 upload.Service。
// @param userID 归属用户 id,由认证中间件解析后显式传入
// @param tokenID 来源 Token 记录 id,0 表示非 Token 途径(后台会话上传)
func (h *Handlers) serveUpload(w http.ResponseWriter, r *http.Request, userID string, tokenID int64) {
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		h.writeReadError(w, err, "无法读取 file 字段(需要 multipart/form-data 的 file 字段)")
		return
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			h.cfg.Logger.Debug("关闭上传文件失败", "err", cerr)
		}
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		h.writeReadError(w, err, "读取上传内容失败")
		return
	}

	// 档位决策:单次表单字段 > 全局默认(在 API 层定死,Service 不做隐式回退)。
	policy := imaging.EXIFPolicy(r.FormValue("exif_policy"))
	if policy == "" {
		policy = h.cfg.DefaultEXIFPolicy
	}

	ttl, ok := h.resolveTTL(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest,
			fmt.Sprintf("ttl 取值非法(需要 0 到 %d 的整数秒,或 never 表示永久)", maxTTLSeconds))
		return
	}

	res, err := h.cfg.Upload.Process(r.Context(), data, upload.Options{
		UserID:         userID,
		TokenID:        tokenID,
		FileName:       header.Filename,
		IdempotencyKey: r.Header.Get(idempotencyHeader),
		EXIFPolicy:     policy,
		TTL:            ttl,
	})
	if err != nil {
		h.writeUploadError(w, err)
		return
	}

	h.cfg.Logger.Info("上传成功", "id", res.ImageID, "file", header.Filename, "size", len(data))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, uploadResponse{
		ID:  res.ImageID,
		URL: h.cfg.BaseURL + "/i/" + res.ImageID,
	})
}

// writeUploadError 把上传服务的业务错误映射为 HTTP 状态码与用户可读消息。
func (h *Handlers) writeUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, upload.ErrUnsupportedType):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnsupportedMediaType, "仅支持图片文件(jpeg/png/gif/webp)")
	case errors.Is(err, upload.ErrTooManyPixels):
		httpx.WriteError(h.cfg.Logger, w, http.StatusUnprocessableEntity,
			fmt.Sprintf("图片分辨率超出限制(像素预算 %d 百万像素)", h.cfg.MaxPixels/megapixel))
	case errors.Is(err, upload.ErrInvalidImage):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "图片内容无法解析")
	case errors.Is(err, imaging.ErrUnknownPolicy):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "exif_policy 取值非法(可选 strip / passthrough)")
	case errors.Is(err, upload.ErrQuotaExceeded):
		httpx.WriteError(h.cfg.Logger, w, http.StatusForbidden, "存储配额已满")
	case errors.Is(err, channel.ErrNoHealthyChannel), errors.Is(err, upload.ErrInsufficientReplicas):
		h.cfg.Logger.Error("渠道不可用,上传失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusServiceUnavailable, "存储渠道暂不可用,请稍后重试")
	default:
		h.cfg.Logger.Error("上传处理失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
	}
}

// megapixel 一百万像素(错误文案换算用)。
const megapixel = 1_000_000

// writeReadError 把请求体读取错误写为带数值的用户可读响应。
// @Description: 超过 MaxBytesReader 上限 → 413 并给出大小上限;其余按客户端请求错误 → 400 用 fallback 文案。
// @param err 读取阶段的原始错误
// @param fallback 非超限错误时的 400 文案
func (h *Handlers) writeReadError(w http.ResponseWriter, err error, fallback string) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		httpx.WriteError(h.cfg.Logger, w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("文件超过大小上限(%d MB)", h.cfg.MaxUploadBytes>>20))
		return
	}
	httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, fallback)
}
