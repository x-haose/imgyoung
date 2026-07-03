package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// metricsMiddleware 记录每个请求的计数与时延,按路由模板打标(非原始路径,避免高基数)。
// @Description: /metrics 与 /healthz 自身不计入,免自我观测噪音;未显式写状态码按 200 计。
// @param next 下游处理器
// @return 包裹后的处理器
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" || route == "/metrics" || route == "/healthz" {
			return
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		s.cfg.Metrics.RecordRequest(r.Method, route, strconv.Itoa(status), time.Since(start))
	})
}
