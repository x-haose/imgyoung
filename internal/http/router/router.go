// Package router 组装 HTTP 路由:数据面、管理面与中间件在此接线成完整处理器。
package router

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// Config 路由组装的全部依赖与参数。
type Config struct {
	Public               *publicapi.Handlers    // 数据面处理器集
	Admin                *adminapi.Handlers     // 管理面处理器集
	Auth                 *auth.Service          // 认证服务(Token 与会话校验)
	UploadRatePerMinute  int                    // 每用户每分钟上传次数上限;<= 0 表示不启用
	MaxConcurrentUploads int                    // 上传并发上限(内存三道闸之一),必须 > 0
	RefererWhitelist     []string               // 防盗链白名单;为空表示不启用
	AllowEmptyReferer    bool                   // 空 Referer 是否放行
	Metrics              *observability.Metrics // 指标集,用于请求计数/时延与 /metrics 暴露
	Web                  fs.FS                  // 后台前端产物;nil 表示不服务页面
	Logger               *slog.Logger           // 结构化日志
}

// New 构建完整路由。
// @Description: 认证面恰好两个——数据 API 走 Bearer Token(scope 校验),
// 管理 API 走 session cookie + CSRF;登录端点自身不在会话门内。
// @param cfg 依赖与参数,字段均必填
// @return 可直接挂载到 http.Server 的处理器
func New(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Metrics(cfg.Metrics))
	r.Use(middleware.SecurityHeaders())
	r.Get("/healthz", cfg.Public.HandleHealth)
	r.Handle("/metrics", cfg.Metrics.Handler())
	// 上传防护闸只此一份,数据面与后台上传共享:频率限制按认证主体的用户 id 计,
	// 并发闸全局共享(峰值内存由总并发决定,与入口无关)。
	uploadRate := middleware.UploadRateLimit(cfg.UploadRatePerMinute, cfg.Logger)
	uploadGate := middleware.ConcurrencyLimit(cfg.MaxConcurrentUploads, cfg.Logger)
	r.Group(func(g chi.Router) {
		g.Use(middleware.TokenAuth(cfg.Auth, "upload", cfg.Logger))
		g.Use(uploadRate)
		g.Use(uploadGate)
		g.Post("/api/upload", cfg.Public.HandleUpload)
	})
	r.Route("/api/admin", func(a chi.Router) {
		// 登录不在会话门内;其自身防护是凭证校验 + 登录限速。
		a.Post("/auth/login", cfg.Admin.HandleLogin)
		a.Group(func(g chi.Router) {
			g.Use(middleware.SessionAuth(cfg.Auth, cfg.Logger))
			g.Use(middleware.CSRF(cfg.Logger))
			g.Post("/auth/logout", cfg.Admin.HandleLogout)
			// 后台上传:所有登录用户可用,与数据面同一上传管道与防护闸。
			g.With(uploadRate, uploadGate).Post("/upload", cfg.Public.HandleSessionUpload)
			g.Get("/me", cfg.Admin.HandleMe)
			g.Put("/me/password", cfg.Admin.HandleChangePassword)
			g.Get("/dashboard", cfg.Admin.HandleDashboard)
			// 运行配置只读切面:上传上限/删除宽限/缩略档位等对普通用户的
			// 操作反馈同样必要,故随会话开放、不进管理员组;不含凭证与密钥。
			g.Get("/config", cfg.Admin.HandleConfig)
			g.Get("/tokens", cfg.Admin.HandleTokenList)
			g.Post("/tokens", cfg.Admin.HandleTokenCreate)
			g.Post("/tokens/{id}/revoke", cfg.Admin.HandleTokenRevoke)
			g.Post("/tokens/{id}/rotate", cfg.Admin.HandleTokenRotate)
			g.Delete("/tokens/{id}", cfg.Admin.HandleTokenDelete)
			g.Get("/images", cfg.Admin.HandleImageList)
			// 后台缩略图/预览走会话鉴权的不计数通道,避免管理员浏览污染
			// 公开访问统计与冷图裁决;对外分享链接仍走 /i/。
			g.Get("/images/{id}/thumb", cfg.Public.HandleAdminServe)
			g.Post("/images/batch-delete", cfg.Admin.HandleImageBatchDelete)
			g.Get("/images/{id}", cfg.Admin.HandleImageGet)
			g.Patch("/images/{id}", cfg.Admin.HandleImagePatch)
			g.Delete("/images/{id}", cfg.Admin.HandleImageDelete)
			// 全站资源(渠道/用户/审计/导出)仅管理员可见可改。
			g.Group(func(ad chi.Router) {
				ad.Use(middleware.RequireAdmin(cfg.Logger))
				ad.Get("/channel-templates", cfg.Admin.HandleChannelTemplates)
				ad.Post("/channels", cfg.Admin.HandleChannelCreate)
				ad.Get("/channels", cfg.Admin.HandleChannelList)
				ad.Post("/channels/test", cfg.Admin.HandleChannelTest)
				ad.Put("/channels/{id}", cfg.Admin.HandleChannelUpdate)
				ad.Get("/channels/{id}/config", cfg.Admin.HandleChannelConfig)
				ad.Post("/channels/{id}/check", cfg.Admin.HandleChannelCheck)
				ad.Delete("/channels/{id}", cfg.Admin.HandleChannelDelete)
				ad.Get("/users", cfg.Admin.HandleUserList)
				ad.Post("/users", cfg.Admin.HandleUserCreate)
				ad.Put("/users/{id}", cfg.Admin.HandleUserUpdate)
				ad.Post("/users/{id}/password", cfg.Admin.HandleUserResetPassword)
				ad.Delete("/users/{id}", cfg.Admin.HandleUserDelete)
				ad.Get("/audit", cfg.Admin.HandleAuditList)
				ad.Get("/export", cfg.Admin.HandleExport)
				ad.Get("/dedup", cfg.Admin.HandleDedupCandidates)
				ad.Post("/dedup/{id}/merge", cfg.Admin.HandleDedupMerge)
				ad.Post("/dedup/{id}/dismiss", cfg.Admin.HandleDedupDismiss)
				ad.Get("/cleanup", cfg.Admin.HandleCleanupList)
				ad.Post("/cleanup/{id}/keep", cfg.Admin.HandleCleanupKeep)
			})
		})
	})
	r.Group(func(g chi.Router) {
		// 防盗链只作用于对外公开图片入口。
		g.Use(middleware.RefererCheck(cfg.RefererWhitelist, cfg.AllowEmptyReferer))
		g.Get("/i/{id}", cfg.Public.HandleServe)
	})
	if cfg.Web != nil {
		// 其余路径交给内嵌 SPA(静态文件 + history 路由回落)。
		r.NotFound(spaHandler(cfg.Web, cfg.Logger))
	}
	return r
}
