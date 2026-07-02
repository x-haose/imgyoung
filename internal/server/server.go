// Package server 实现 API 层:上传、访问、鉴权与防盗链(设计定稿·整体分层架构)。
// 业务编排在 Service 层(internal/upload 等),本包只做 HTTP 翻译。
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/storage"
	"github.com/x-haose/imgyoung/internal/upload"
)

// Config Server 的全部依赖与参数。
type Config struct {
	// Queries sqlc 生成的类型安全查询集(访问路径用)。
	Queries *db.Queries
	// Driver 目标渠道的存储驱动(访问路径用)。
	Driver storage.Driver
	// Upload 上传编排服务。
	Upload *upload.Service
	// Token 上传鉴权 Token(迭代 0–4:单 Token;多 Token 体系见迭代 5)。
	Token string
	// MaxUploadBytes 单图字节上限(内存三道闸之一)。
	MaxUploadBytes int64
	// BaseURL 对外站点地址(无尾部斜杠);为空时响应返回相对路径。
	BaseURL string
	// RefererWhitelist 防盗链白名单;为空表示不启用。
	RefererWhitelist []string
	// AllowEmptyReferer 空 Referer 是否放行。
	AllowEmptyReferer bool
	// Logger 结构化日志。
	Logger *slog.Logger
}

// Server HTTP 服务,持有全部依赖,自身无可变状态、并发安全。
type Server struct {
	cfg Config
}

// New 创建 Server。
// @param cfg 依赖与参数,字段均必填
// @return 就绪的 Server
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Handler 构建完整路由。
// @return 可直接挂载到 http.Server 的处理器
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.handleHealth)
	r.Group(func(g chi.Router) {
		g.Use(s.requireToken)
		g.Post("/api/upload", s.handleUpload)
	})
	r.Group(func(g chi.Router) {
		// 防盗链只作用于对外公开图片入口(设计定稿·区分后台浏览 vs 真实访问)。
		g.Use(s.checkReferer)
		g.Get("/i/{id}", s.handleServe)
	})
	return r
}
