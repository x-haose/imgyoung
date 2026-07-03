package server

import "net/http"

// limitConcurrency 并发闸中间件:同时处理的请求数不超过 n,满载立即 429。
// @Description: 内存三道闸之一——上传解码的峰值内存
// 由"并发数 × 解码位图大小"决定,必须有硬上限。满载快速失败并带
// Retry-After,把排队决策交给客户端。
// @param n 最大并发数,必须 > 0
// @param next 被保护的处理器
func (s *Server) limitConcurrency(n int, next http.Handler) http.Handler {
	sem := make(chan struct{}, n)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			s.writeError(w, http.StatusTooManyRequests, "上传并发已满,请稍后重试")
		}
	})
}
