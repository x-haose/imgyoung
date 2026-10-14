package middleware

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

// refererAllowed 判定请求来源是否可放行:无 Referer 按 allowEmpty,有则其主机须命中白名单。
// @Description: Referer 解析失败视为未命中——畸形来源与陌生来源同样不放行。
// @param whitelist 白名单模式列表,非空
// @param ref 请求 Referer 头原文,可为空
// @param allowEmpty 空 Referer 是否放行
// @return 是否放行
func refererAllowed(whitelist []string, ref string, allowEmpty bool) bool {
	if ref == "" {
		return allowEmpty
	}
	u, err := url.Parse(ref)
	return err == nil && matchRefererHost(whitelist, u.Hostname())
}

// RefererCheck 防盗链中间件。
// @Description: 白名单与空 Referer 策略逐请求取快照,变更即时生效(热生效);
// patterns 为 nil 或返回空 = 不启用;空 Referer 按 allowEmpty 决定;
// Referer 主机未命中白名单返回 403。仅挂在对外图片访问路由上。
// @param patterns 白名单快照读取口(须并发安全)
// @param allowEmpty 空 Referer 放行开关读取口
// @return 中间件
func RefererCheck(patterns func() []string, allowEmpty func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var whitelist []string
			if patterns != nil {
				whitelist = patterns()
			}
			if len(whitelist) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if !refererAllowed(whitelist, r.Header.Get("Referer"), allowEmpty()) {
				http.Error(w, "禁止访问", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
