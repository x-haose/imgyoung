package middleware

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// SessionCookie 后台会话 cookie 名。
const SessionCookie = "imgyoung_session"

// CSRFHeader 状态变更请求须携带的 CSRF 令牌请求头。
const CSRFHeader = "X-CSRF-Token"

// ctxKey 请求上下文键的私有类型,防跨包碰撞。
type ctxKey int

const (
	ctxKeyTokenPrincipal ctxKey = iota // Token 认证主体
	ctxKeySession                      // 会话信息
)

// TokenPrincipalFrom 取出 Token 认证主体。
// @param ctx 请求上下文
// @return 主体与是否存在
func TokenPrincipalFrom(ctx context.Context) (auth.TokenPrincipal, bool) {
	tp, ok := ctx.Value(ctxKeyTokenPrincipal).(auth.TokenPrincipal)
	return tp, ok
}

// WithTokenPrincipal 把 Token 主体写入上下文。
// @Description: 供在中间件外完成鉴权的协议面(如 S3 的 SigV4)接入同一套主体传递,
// 使其后的频控等中间件与 Bearer 路径取到的是同一个主体。
// @param ctx 请求上下文
// @param tp Token 主体
// @return 携带主体的上下文
func WithTokenPrincipal(ctx context.Context, tp auth.TokenPrincipal) context.Context {
	return context.WithValue(ctx, ctxKeyTokenPrincipal, tp)
}

// SessionFrom 取出会话信息。
// @param ctx 请求上下文
// @return 会话信息与是否存在
func SessionFrom(ctx context.Context) (auth.SessionInfo, bool) {
	si, ok := ctx.Value(ctxKeySession).(auth.SessionInfo)
	return si, ok
}

// TokenAuth 数据 API 鉴权中间件:校验 Bearer Token 并要求具备指定 scope。
// @Description: Token 唯一真相在 DB(哈希比对);通过后把主体放入请求上下文,
// handler 取出后显式传参进 core。查库失败返回 500,不当作 Token 无效。
// @param svc 认证服务
// @param scope 本路由要求的授权范围
// @param l 结构化日志
// @return 中间件
func TokenAuth(svc *auth.Service, scope string, l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			plaintext, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
			if !ok {
				httpx.WriteError(l, w, http.StatusUnauthorized, errcode.UploadTokenInvalid)
				return
			}
			tp, err := svc.AuthenticateToken(r.Context(), plaintext)
			switch {
			case errors.Is(err, auth.ErrTokenInvalid):
				httpx.WriteError(l, w, http.StatusUnauthorized, errcode.UploadTokenInvalid)
				return
			case err != nil:
				l.Error("校验 Token 失败", "err", err)
				httpx.WriteError(l, w, http.StatusInternalServerError, errcode.Internal)
				return
			}
			if !slices.Contains(strings.Split(tp.Scopes, ","), scope) {
				httpx.WriteError(l, w, http.StatusForbidden, errcode.UploadTokenScope)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyTokenPrincipal, tp)))
		})
	}
}

// SessionAuth 管理面鉴权中间件:校验会话 cookie。
// @Description: 无 cookie、会话过期或用户被禁用一律 401,查库失败返回 500;通过后把会话信息放入
// 请求上下文。管理面不认 Token。
// @param svc 认证服务
// @param l 结构化日志
// @return 中间件
func SessionAuth(svc *auth.Service, l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookie)
			if err != nil || c.Value == "" {
				httpx.WriteError(l, w, http.StatusUnauthorized, errcode.Unauthenticated)
				return
			}
			info, err := svc.ValidateSession(r.Context(), c.Value)
			switch {
			case errors.Is(err, auth.ErrSessionInvalid):
				httpx.WriteError(l, w, http.StatusUnauthorized, errcode.Unauthenticated)
				return
			case err != nil:
				l.Error("校验会话失败", "err", err)
				httpx.WriteError(l, w, http.StatusInternalServerError, errcode.Internal)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeySession, info)))
		})
	}
}

// CSRF 状态变更防护中间件:非安全方法必须回传与会话绑定的 CSRF 令牌。
// @Description: 须挂在 SessionAuth 之后;GET/HEAD/OPTIONS 直接放行,
// 其余方法常数时间比较 X-CSRF-Token 头与会话令牌。
// @param l 结构化日志
// @return 中间件
func CSRF(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			info, ok := SessionFrom(r.Context())
			if !ok || subtle.ConstantTimeCompare(
				[]byte(r.Header.Get(CSRFHeader)), []byte(info.CSRFToken)) != 1 {
				httpx.WriteError(l, w, http.StatusForbidden, errcode.CSRFRejected)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin 管理员门槛中间件:非 admin 角色一律 403。
// @Description: 须挂在 SessionAuth 之后。
// @param l 结构化日志
// @return 中间件
func RequireAdmin(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := SessionFrom(r.Context())
			if !ok || !info.Principal.IsAdmin() {
				httpx.WriteError(l, w, http.StatusForbidden, errcode.AdminRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePasswordChanged 初始密码门槛:仍用初始密码的会话一律 403。
// @Description: 须挂在 SessionAuth 之后。初始密码写在日志或部署配置里,改掉它之前会话只该能退出、查看自己与改密;
// 签发 Token、建渠道、上传等接口一概不放行,不论请求来自后台页面还是脚本。
// @param l 结构化日志
// @return 中间件
func RequirePasswordChanged(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := SessionFrom(r.Context())
			if !ok || info.Principal.MustChangePassword {
				httpx.WriteError(l, w, http.StatusForbidden, errcode.MustChangePassword)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders 全局安全响应头中间件。
// @Description: nosniff 强制信任服务端类型判定;DENY 拒绝被嵌入 frame(防点击劫持);
// Referrer-Policy 限制跨站 referrer 泄露。对所有响应生效。
// @return 中间件
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			next.ServeHTTP(w, r)
		})
	}
}
