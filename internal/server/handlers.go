package server

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/id"
	"github.com/x-haose/imgyoung/internal/storage"
)

// bearerPrefix Authorization 头的 Bearer 方案前缀。
const bearerPrefix = "Bearer "

// uploadResponse 上传成功的响应体。
type uploadResponse struct {
	// ID 对外短 ID。
	ID string `json:"id"`
	// URL 图片访问路径(相对路径,由客户端拼接站点地址)。
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

// handleUpload 处理图片上传:限长 → 取文件 → magic bytes 校验 → 存渠道 → 落库。
// @Description: 迭代 0 最小链路;哈希/净化/查重等按迭代计划后续接入。
// 整个文件缓冲在内存中,上限由 MaxUploadBytes 保证(设计定稿·内存三道闸)。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)

	file, _, err := r.FormFile("file")
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

	// 只信 magic bytes,不信扩展名与客户端声明(设计定稿·格式校验)。
	// SVG 不会被嗅探为 image/*,天然被拒(设计定稿·格式安全)。
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		s.writeError(w, http.StatusUnsupportedMediaType, "仅支持图片文件")
		return
	}

	newID, err := id.New()
	if err != nil {
		s.cfg.Logger.Error("生成短 ID 失败", "err", err)
		s.writeError(w, http.StatusInternalServerError, "服务内部错误")
		return
	}

	// 迭代 0:对象键 = 短 ID;迭代 1 起改为 {short_id}_{hash8}(设计定稿·命名)。
	if err := s.cfg.Driver.Put(r.Context(), newID, bytes.NewReader(data), int64(len(data))); err != nil {
		s.cfg.Logger.Error("写入渠道失败", "id", newID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "存储失败")
		return
	}
	if err := s.cfg.Queries.CreateImage(r.Context(), db.CreateImageParams{
		ID:         newID,
		StorageKey: newID,
		Mime:       mime,
		Size:       int64(len(data)),
	}); err != nil {
		s.cfg.Logger.Error("写入引用记录失败", "id", newID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "存储失败")
		return
	}

	s.cfg.Logger.Info("上传成功", "id", newID, "mime", mime, "size", len(data))
	s.writeJSON(w, http.StatusOK, uploadResponse{ID: newID, URL: "/i/" + newID})
}

// handleServe 按短 ID 代理输出图片(设计定稿·默认代理)。
func (s *Server) handleServe(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "id")

	img, err := s.cfg.Queries.GetImage(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.cfg.Logger.Error("查询引用记录失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}

	rc, err := s.cfg.Driver.Open(r.Context(), img.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		// DB 有记录但渠道无对象:数据不一致,记录告警并对外 404(对账任务见迭代 3)。
		s.cfg.Logger.Warn("引用存在但渠道对象缺失", "id", imageID, "key", img.StorageKey)
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
	h.Set("Content-Type", img.Mime)
	h.Set("Content-Length", strconv.FormatInt(img.Size, 10))
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
