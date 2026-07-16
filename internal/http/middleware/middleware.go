// Package middleware 提供路由无关的 HTTP 中间件:鉴权、防盗链、并发闸与指标。
package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// bearerPrefix Authorization 头的 Bearer 方案前缀。
const bearerPrefix = "Bearer "

// RequireToken 上传鉴权中间件。
// @Description: 校验 Authorization: Bearer <token>,常数时间比较防时序侧信道。
// @param token 期望的 Token 明文
// @param l 结构化日志
// @return 中间件
func RequireToken(token string, l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				httpx.WriteError(l, w, http.StatusUnauthorized, "无效的上传 Token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ConcurrencyLimit 并发闸中间件:同时处理的请求数不超过 n,满载立即 429。
// @Description: 内存三道闸之一——上传解码的峰值内存由"并发数 × 解码位图大小"
// 决定,必须有硬上限。满载快速失败并带 Retry-After,把排队决策交给客户端。
// @param n 最大并发数,必须 > 0
// @param l 结构化日志
// @return 中间件
func ConcurrencyLimit(n int, l *slog.Logger) func(http.Handler) http.Handler {
	sem := make(chan struct{}, n)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Retry-After", "1")
				httpx.WriteError(l, w, http.StatusTooManyRequests, "上传并发已满,请稍后重试")
			}
		})
	}
}

// Metrics 请求指标中间件,按路由模板打标(非原始路径,避免高基数)。
// @Description: /metrics 与 /healthz 自身不计入,免自我观测噪音;未显式写状态码按 200 计。
// @param m 指标集
// @return 中间件
func Metrics(m *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" || route == "/metrics" || route == "/healthz" {
				return
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			m.RecordRequest(r.Method, route, strconv.Itoa(status), time.Since(start))
		})
	}
}
