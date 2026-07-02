// Package server 实现 API 层:上传、访问与鉴权(设计定稿·整体分层架构)。
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/storage"
)

// Config Server 的全部依赖与参数。
type Config struct {
	// Queries sqlc 生成的类型安全查询集。
	Queries *db.Queries
	// Driver 目标渠道的存储驱动。
	Driver storage.Driver
	// Token 上传鉴权 Token(迭代 0:单 Token;多 Token 体系见迭代 5)。
	Token string
	// MaxUploadBytes 单图字节上限(内存三道闸之一)。
	MaxUploadBytes int64
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
	r.Get("/i/{id}", s.handleServe)
	return r
}
