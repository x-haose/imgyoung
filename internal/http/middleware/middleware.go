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
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// bearerPrefix Authorization 头的 Bearer 方案前缀。
const bearerPrefix = "Bearer "

// Rejecter 写出防护闸拒绝请求的响应;各协议面的错误体格式不同(接口错误信封、S3 的 XML 错误体)。
// @param status HTTP 状态码
// @param code 错误码
type Rejecter func(w http.ResponseWriter, r *http.Request, status int, code errcode.Code)

// JSONRejecter 以接口错误信封写出拒绝。
// @param l 结构化日志
// @return 拒绝响应的写法
func JSONRejecter(l *slog.Logger) Rejecter {
	return func(w http.ResponseWriter, _ *http.Request, status int, code errcode.Code) {
		httpx.WriteError(l, w, status, code)
	}
}

// ConcurrencyGate 并发闸:同时处理的请求数不超过上限,满载立即 429。
// @Description: 内存三道闸之一——上传解码的峰值内存由"并发数 × 解码位图大小"
// 决定,必须有硬上限。满载快速失败并带 Retry-After,把排队决策交给客户端。
// 上限逐请求取值,后台调整热生效。各协议面经 Middleware 挂同一个闸,共用一份计数。
type ConcurrencyGate struct {
	limit  func() int // 最大并发数读取口,取值必须 > 0
	mu     sync.Mutex // 保护 active
	active int        // 正在处理的请求数
}

// NewConcurrencyGate 创建并发闸。
// @param limit 最大并发数读取口,取值必须 > 0
// @return 并发闸
func NewConcurrencyGate(limit func() int) *ConcurrencyGate {
	return &ConcurrencyGate{limit: limit}
}

// Middleware 返回挂在某个协议面上的中间件。
// @param reject 该协议面写出拒绝的方式
// @return 中间件
func (g *ConcurrencyGate) Middleware(reject Rejecter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !g.acquire() {
				w.Header().Set("Retry-After", "1")
				reject(w, r, http.StatusTooManyRequests, errcode.UploadBusy)
				return
			}
			defer g.release()
			next.ServeHTTP(w, r)
		})
	}
}

// acquire 占用一个处理名额。
// @return 未满载占到名额为 true
func (g *ConcurrencyGate) acquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= g.limit() {
		return false
	}
	g.active++
	return true
}

// release 归还 acquire 占到的名额。
func (g *ConcurrencyGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
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
