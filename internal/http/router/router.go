// Package router 组装 HTTP 路由:数据面、管理面与中间件在此接线成完整处理器。
package router

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/http/s3api"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// Config 路由组装的全部依赖与参数。
type Config struct {
	Public          *publicapi.Handlers    // 数据面处理器集
	Admin           *adminapi.Handlers     // 管理面处理器集
	S3              *s3api.Handlers        // S3 协议面处理器集;nil 表示不开放该入口
	Auth            *auth.Service          // 认证服务(Token 与会话校验)
	Settings        func() config.Settings // 运营配置快照读取口(逐请求取值,热生效);上传频率/并发上限/空 Referer 策略出自它
	RefererPatterns func() []string        // 防盗链白名单快照读取口(热生效);nil 或返回空表示不启用
	Metrics         *observability.Metrics // 指标集,用于请求计数/时延与 /metrics 暴露
	TrustedProxies  []netip.Prefix         // 连接对端落在这些网段时才采信 X-Forwarded-* 转发头;空表示一概不信
	MetricsAllow    []netip.Prefix         // 客户端地址落在这些网段时才放行 /metrics;空表示对谁都不开放
	Web             fs.FS                  // 后台前端产物;nil 表示不服务页面
	Logger          *slog.Logger           // 结构化日志
}

// uploadGuards 上传防护闸,只此一份,数据面、后台上传与 S3 入口共享:
// 频率限制按认证主体的用户 id 计,并发闸全局共享(峰值内存由总并发决定,与入口无关)。
type uploadGuards struct {
	rate func(http.Handler) http.Handler // 按用户的上传频率限制
	gate func(http.Handler) http.Handler // 全局上传并发闸
}

// New 构建完整路由。
// @Description: 认证面恰好两个——数据 API 走 Bearer Token(scope 校验),
// 管理 API 走 session cookie + CSRF;登录端点自身不在会话门内。
// @param cfg 依赖与参数,字段均必填
// @return 可直接挂载到 http.Server 的处理器
func New(cfg Config) http.Handler {
	r := chi.NewRouter()
	// 来源判定最先挂:之后的限速键、Secure cookie、对外地址推导与 /metrics 门槛都只认它。
	r.Use(middleware.ProxyHeaders(cfg.TrustedProxies))
	r.Use(middleware.Metrics(cfg.Metrics))
	r.Use(middleware.SecurityHeaders())
	r.Get("/healthz", cfg.Public.HandleHealth)
	r.Get("/api/openapi.yaml", openAPIHandler())
	r.With(middleware.AllowFrom(cfg.MetricsAllow, cfg.Logger)).Handle("/metrics", cfg.Metrics.Handler())
	guards := uploadGuards{
		rate: middleware.UploadRateLimit(func() int { return cfg.Settings().UploadRatePerMin }, cfg.Logger),
		gate: middleware.ConcurrencyLimit(func() int { return cfg.Settings().MaxConcurrentUploads }, cfg.Logger),
	}
	r.Group(func(g chi.Router) {
		g.Use(middleware.TokenAuth(cfg.Auth, "upload", cfg.Logger))
		g.Use(guards.rate, guards.gate)
		g.Post("/api/upload", cfg.Public.HandleUpload)
		// URL 转存与直传共用同一管道与防护闸,只是字节来自远端抓取。
		g.Post("/api/upload/url", cfg.Public.HandleUploadURL)
	})
	r.Route("/api/admin", func(a chi.Router) { mountAdmin(a, cfg, guards) })
	r.Group(func(g chi.Router) {
		// 防盗链只作用于对外公开图片入口;S3 对象键别名与短 ID 是同一张图的两个门,同守一道闸。
		g.Use(middleware.RefererCheck(cfg.RefererPatterns, func() bool { return cfg.Settings().AllowEmptyReferer }))
		g.Get("/i/{id}", cfg.Public.HandleServe)
		g.Get("/o/*", cfg.Public.HandleServeObjectKey)
		// HEAD 与 GET 同一处理:只写响应头,不打开渠道对象,也不计访问
		g.Head("/i/{id}", cfg.Public.HandleServe)
		g.Head("/o/*", cfg.Public.HandleServeObjectKey)
	})
	if cfg.S3 != nil {
		r.Route("/s3", func(s3 chi.Router) { mountS3(s3, cfg, guards) })
	}
	if cfg.Web != nil {
		// 其余路径交给内嵌 SPA(静态文件 + history 路由回落)。
		r.NotFound(spaHandler(cfg.Web, cfg.Logger))
	}
	return r
}

// mountAdmin 挂管理面:登录端点在会话门外,其余经会话 + CSRF;全站资源再套管理员门。
// @param a /api/admin 下的子路由
// @param cfg 路由依赖
// @param guards 上传防护闸
func mountAdmin(a chi.Router, cfg Config, guards uploadGuards) {
	// 登录不在会话门内;其自身防护是凭证校验 + 登录限速。
	a.Post("/auth/login", cfg.Admin.HandleLogin)
	a.Group(func(g chi.Router) {
		g.Use(middleware.SessionAuth(cfg.Auth, cfg.Logger))
		g.Use(middleware.CSRF(cfg.Logger))
		g.Post("/auth/logout", cfg.Admin.HandleLogout)
		// 后台上传:所有登录用户可用,与数据面同一上传管道与防护闸。
		g.With(guards.rate, guards.gate).Post("/upload", cfg.Public.HandleSessionUpload)
		g.With(guards.rate, guards.gate).Post("/upload/url", cfg.Public.HandleSessionUploadURL)
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
		// 取长期密钥用 POST:CSRF 中间件放行全部 GET,凭证下发不该走在防护之外。
		g.Post("/tokens/{id}/s3-credential", cfg.Admin.HandleTokenS3Credential)
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
			mountAdminOnly(ad, cfg)
		})
	})
}

// mountAdminOnly 挂仅管理员可用的全站资源:渠道、用户、配置、防盗链、审计、导出、查重与清理裁决。
// @param ad 已套管理员门的子路由
// @param cfg 路由依赖
func mountAdminOnly(ad chi.Router, cfg Config) {
	ad.Get("/channel-templates", cfg.Admin.HandleChannelTemplates)
	// 授权回调由第三方顶层跳转打回,GET 不过 CSRF 闸,SameSite=Lax 下会话仍随行。
	ad.Post("/channel-oauth/start", cfg.Admin.HandleChannelOAuthStart)
	ad.Get("/channel-oauth/callback", cfg.Admin.HandleChannelOAuthCallback)
	ad.Get("/channel-oauth/result", cfg.Admin.HandleChannelOAuthResult)
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
	ad.Get("/config/full", cfg.Admin.HandleConfigFull)
	ad.Put("/config", cfg.Admin.HandleConfigUpdate)
	ad.Get("/referer-whitelist", cfg.Admin.HandleRefererList)
	ad.Post("/referer-whitelist", cfg.Admin.HandleRefererAdd)
	ad.Delete("/referer-whitelist/{id}", cfg.Admin.HandleRefererDelete)
	ad.Get("/audit", cfg.Admin.HandleAuditList)
	ad.Get("/alerts", cfg.Admin.HandleAlertList)
	ad.Get("/backups", cfg.Admin.HandleBackupList)
	ad.Post("/backups", cfg.Admin.HandleBackupCreate)
	ad.Get("/backups/{name}", cfg.Admin.HandleBackupDownload)
	ad.Post("/backups/{name}/restore", cfg.Admin.HandleBackupRestore)
	ad.Get("/export", cfg.Admin.HandleExport)
	ad.Get("/dedup", cfg.Admin.HandleDedupCandidates)
	ad.Post("/dedup/{id}/merge", cfg.Admin.HandleDedupMerge)
	ad.Post("/dedup/{id}/dismiss", cfg.Admin.HandleDedupDismiss)
	// 冷图裁决没有专用写端点:保留走 PATCH /images/{id} 置清理豁免,删除走 DELETE /images/{id}。
	ad.Get("/cleanup", cfg.Admin.HandleCleanupList)
}

// mountS3 挂 S3 协议面。
// @Description: 收在 /s3 前缀下:S3 的桶路径本是顶层单段,直接挂根会吃掉 /login 这类
// SPA 路由(命中却方法不符 → 405,再不回落到前端)。客户端把端点配成 <站点>/s3 即可。
// 自带 SigV4 签名鉴权(凭证仍是同一批上传 Token),故不套 TokenAuth;上传防护闸共享。
// 闸门顺序:并发闸挡在验签之前(未验签请求也不该无界并发地打数据库),验签产生主体后才轮到按用户计的频控。
// @param s3 /s3 下的子路由
// @param cfg 路由依赖
// @param guards 上传防护闸
func mountS3(s3 chi.Router, cfg Config, guards uploadGuards) {
	s3.Use(guards.gate, cfg.S3.Authenticate)
	s3.With(guards.rate).Put("/{bucket}/*", cfg.S3.HandlePutObject)
	s3.Head("/{bucket}", cfg.S3.HandleHeadBucket)
	// 读与删不是上传,不过上传频控;并发闸与验签照过。
	s3.Head("/{bucket}/*", cfg.S3.HandleHeadObject)
	s3.Get("/{bucket}/*", cfg.S3.HandleGetObject)
	s3.Delete("/{bucket}/*", cfg.S3.HandleDeleteObject)
}
