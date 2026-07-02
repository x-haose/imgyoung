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

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/imaging"
	"github.com/x-haose/imgyoung/internal/storage"
	"github.com/x-haose/imgyoung/internal/upload"
)

// bearerPrefix Authorization 头的 Bearer 方案前缀。
const bearerPrefix = "Bearer "

// idempotencyHeader 客户端幂等键请求头。
const idempotencyHeader = "Idempotency-Key"

// uploadResponse 上传成功的响应体。
type uploadResponse struct {
	// ID 对外短 ID。
	ID string `json:"id"`
	// URL 图片访问地址;配置了 base_url 时为绝对地址,否则为相对路径。
	URL string `json:"url"`
}

// errorResponse API 错误的统一信封。
type errorResponse struct {
	// Error 面向用户的错误描述,不泄露内部细节。
	Error string `json:"error"`
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

// handleUpload 处理图片上传:HTTP 解包 → 交给上传服务 → 翻译结果。
// @Description: 业务管道(格式校验/像素预算/净化/哈希/落库)在 upload.Service;
// 本函数只负责 HTTP 层:大小闸、multipart 解析、错误码映射。
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

	res, err := s.cfg.Upload.Process(r.Context(), data, upload.Options{
		FileName:       header.Filename,
		IdempotencyKey: r.Header.Get(idempotencyHeader),
		EXIFPolicy:     policy,
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

// handleServe 按短 ID 代理输出图片(设计定稿·默认代理)。
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

	rc, err := s.cfg.Driver.Open(r.Context(), row.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		// DB 有记录但渠道无对象:数据不一致,记录告警并对外 404(对账任务见迭代 3)。
		s.cfg.Logger.Warn("引用存在但渠道对象缺失", "id", imageID, "key", row.StorageKey)
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.cfg.Logger.Error("打开渠道对象失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
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
	// 内容不可变(URL 即版本),缓存一年;nosniff 强制浏览器信任服务端类型判定。
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
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
