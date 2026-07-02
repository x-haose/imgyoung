// Package server 实现 API 层:上传、访问、鉴权与防盗链(设计定稿·整体分层架构)。
// 业务编排在 Service 层(internal/upload 等),本包只做 HTTP 翻译。
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/channel"
	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/imaging"
	"github.com/x-haose/imgyoung/internal/upload"
)

// BlobChannels 按 blob 提供可服务的渠道候选(由 channel.Manager 实现)。
type BlobChannels interface {
	// CandidatesForBlob 返回健康且副本就绪的渠道,按优先级降序。
	CandidatesForBlob(ctx context.Context, blobID int64) ([]channel.Handle, error)
}

// Config Server 的全部依赖与参数。
type Config struct {
	// Queries sqlc 生成的类型安全查询集(访问路径用)。
	Queries *db.Queries
	// Channels 访问路径的渠道候选来源(容灾切换依据)。
	Channels BlobChannels
	// Upload 上传编排服务。
	Upload *upload.Service
	// Token 上传鉴权 Token,单 Token 模型(多 Token 体系见设计定稿·鉴权与滥用防护)。
	Token string
	// MaxUploadBytes 单图字节上限(内存三道闸之一)。
	MaxUploadBytes int64
	// MaxConcurrentUploads 上传并发上限(内存三道闸之一),必须 > 0。
	MaxConcurrentUploads int
	// BaseURL 对外站点地址(无尾部斜杠);为空时响应返回相对路径。
	BaseURL string
	// DefaultEXIFPolicy 全局默认 EXIF 档位;单次上传的表单字段可覆盖。
	// 档位决策在本层完成,传给 Service 的必须是明确档位。
	DefaultEXIFPolicy imaging.EXIFPolicy
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
		g.Use(func(next http.Handler) http.Handler {
			return s.limitConcurrency(s.cfg.MaxConcurrentUploads, next)
		})
		g.Post("/api/upload", s.handleUpload)
	})
	r.Group(func(g chi.Router) {
		// 防盗链只作用于对外公开图片入口(设计定稿·区分后台浏览 vs 真实访问)。
		g.Use(s.checkReferer)
		g.Get("/i/{id}", s.handleServe)
	})
	return r
}
