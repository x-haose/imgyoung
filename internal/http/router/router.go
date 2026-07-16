// Package router 组装 HTTP 路由:数据面、管理面与中间件在此接线成完整处理器。
package router

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// Config 路由组装的全部依赖与参数。
type Config struct {
	Public               *publicapi.Handlers    // 数据面处理器集
	Admin                *adminapi.Handlers     // 管理面处理器集
	Token                string                 // 上传鉴权 Token,单 Token 模型
	MaxConcurrentUploads int                    // 上传并发上限(内存三道闸之一),必须 > 0
	RefererWhitelist     []string               // 防盗链白名单;为空表示不启用
	AllowEmptyReferer    bool                   // 空 Referer 是否放行
	Metrics              *observability.Metrics // 指标集,用于请求计数/时延与 /metrics 暴露
	Logger               *slog.Logger           // 结构化日志
}

// New 构建完整路由。
// @param cfg 依赖与参数,字段均必填
// @return 可直接挂载到 http.Server 的处理器
func New(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Metrics(cfg.Metrics))
	r.Get("/healthz", cfg.Public.HandleHealth)
	r.Handle("/metrics", cfg.Metrics.Handler())
	requireToken := middleware.RequireToken(cfg.Token, cfg.Logger)
	r.Group(func(g chi.Router) {
		g.Use(requireToken)
		g.Use(middleware.ConcurrencyLimit(cfg.MaxConcurrentUploads, cfg.Logger))
		g.Post("/api/upload", cfg.Public.HandleUpload)
	})
	r.Group(func(g chi.Router) {
		g.Use(requireToken)
		g.Post("/api/channels", cfg.Admin.HandleChannelCreate)
		g.Get("/api/channels", cfg.Admin.HandleChannelList)
		g.Delete("/api/channels/{id}", cfg.Admin.HandleChannelDelete)
	})
	r.Group(func(g chi.Router) {
		// 防盗链只作用于对外公开图片入口。
		g.Use(middleware.RefererCheck(cfg.RefererWhitelist, cfg.AllowEmptyReferer))
		g.Get("/i/{id}", cfg.Public.HandleServe)
	})
	return r
}
