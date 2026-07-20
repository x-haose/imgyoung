// Package middleware 提供路由无关的 HTTP 中间件:鉴权、防盗链、并发闸与指标。
package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// bearerPrefix Authorization 头的 Bearer 方案前缀。
const bearerPrefix = "Bearer "

// ConcurrencyLimit 并发闸中间件:同时处理的请求数不超过上限,满载立即 429。
// @Description: 内存三道闸之一——上传解码的峰值内存由"并发数 × 解码位图大小"
// 决定,必须有硬上限。满载快速失败并带 Retry-After,把排队决策交给客户端。
// 上限逐请求取值,后台调整热生效。
// @param limit 最大并发数读取口,取值必须 > 0
// @param l 结构化日志
// @return 中间件
func ConcurrencyLimit(limit func() int, l *slog.Logger) func(http.Handler) http.Handler {
	var mu sync.Mutex
	active := 0
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			if active >= limit() {
				mu.Unlock()
				w.Header().Set("Retry-After", "1")
				httpx.WriteError(l, w, http.StatusTooManyRequests, "上传并发已满,请稍后重试")
				return
			}
			active++
			mu.Unlock()
			defer func() {
				mu.Lock()
				active--
				mu.Unlock()
			}()
			next.ServeHTTP(w, r)
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
