package server

import (
	"net/http"
	"net/url"
	"strings"
)

// matchRefererHost 判断请求来源主机是否命中防盗链白名单。
// @Description: 模式支持两种——精确域名("blog.foo.dev")与子域通配
// ("*.example.com",仅匹配子域、不匹配裸域);匹配大小写不敏感。
// @param patterns 白名单模式列表
// @param host 来源主机名(不含端口)
// @return 是否命中
func matchRefererHost(patterns []string, host string) bool {
	host = strings.ToLower(host)
	for _, p := range patterns {
		p = strings.ToLower(p)
		if base, ok := strings.CutPrefix(p, "*."); ok {
			if strings.HasSuffix(host, "."+base) {
				return true
			}
			continue
		}
		if host == p {
			return true
		}
	}
	return false
}

// checkReferer 防盗链中间件(设计定稿·防盗链白名单)。
// @Description: 白名单为空 = 不启用;空 Referer 按 AllowEmptyReferer 决定;
// Referer 主机未命中白名单返回 403。仅挂在对外图片访问路由上。
func (s *Server) checkReferer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.cfg.RefererWhitelist) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		ref := r.Header.Get("Referer")
		if ref == "" {
			if s.cfg.AllowEmptyReferer {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "禁止访问", http.StatusForbidden)
			return
		}
		u, err := url.Parse(ref)
		if err != nil || !matchRefererHost(s.cfg.RefererWhitelist, u.Hostname()) {
			http.Error(w, "禁止访问", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
