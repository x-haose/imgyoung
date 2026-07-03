// Package server 实现 API 层:上传、访问、鉴权与防盗链(设计定稿·整体分层架构)。
// 业务编排在 Service 层(internal/upload 等),本包只做 HTTP 翻译。
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/observability"
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
	// Admin 渠道管理操作。
	Admin ChannelAdmin
	// EncryptNewChannels 新建渠道未指定 encrypted 时的全局默认(创建时固化)。
	EncryptNewChannels bool
	// RedirectMode 302 省流量模式:候选渠道允许直链时重定向,否则回退代理。
	RedirectMode bool
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
	// DefaultTTL 全局默认 TTL;<=0 表示永不过期。单次上传的 ttl 字段可覆盖。
	DefaultTTL time.Duration
	// Metrics 指标集,用于请求计数/时延与 /metrics 暴露。
	Metrics *observability.Metrics
	// Audit 管理操作审计记录器。
	Audit *audit.Recorder
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
	r.Use(s.metricsMiddleware)
	r.Get("/healthz", s.handleHealth)
	r.Handle("/metrics", s.cfg.Metrics.Handler())
	r.Group(func(g chi.Router) {
		g.Use(s.requireToken)
		g.Use(func(next http.Handler) http.Handler {
			return s.limitConcurrency(s.cfg.MaxConcurrentUploads, next)
		})
		g.Post("/api/upload", s.handleUpload)
	})
	r.Group(func(g chi.Router) {
		g.Use(s.requireToken)
		g.Post("/api/channels", s.handleChannelCreate)
		g.Get("/api/channels", s.handleChannelList)
		g.Delete("/api/channels/{id}", s.handleChannelDelete)
	})
	r.Group(func(g chi.Router) {
		// 防盗链只作用于对外公开图片入口(设计定稿·区分后台浏览 vs 真实访问)。
		g.Use(s.checkReferer)
		g.Get("/i/{id}", s.handleServe)
	})
	return r
}
