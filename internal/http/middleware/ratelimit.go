package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// rateLimiterMaxEntries 频控键表容量上限,超出即触发闲置回收,防内存无界。
const rateLimiterMaxEntries = 4096

// rateLimiterEntryTTL 频控键闲置超过该时长视为可回收。
const rateLimiterEntryTTL = time.Hour

// rateEntry 单个用户的令牌桶与最近活跃时刻。
type rateEntry struct {
	lim       *rate.Limiter // 该用户的令牌桶
	perMinute int           // 建桶时的每分钟额度;额度热调整后按当前值重建
	lastSeen  time.Time     // 最近一次请求时刻,闲置回收依据
}

// uploadSubject 解析上传请求的认证主体用户 id:Token 主体或会话主体皆可。
// @param r 已过认证中间件的请求
// @return 用户 id 与是否存在
func uploadSubject(r *http.Request) (string, bool) {
	if tp, ok := TokenPrincipalFrom(r.Context()); ok {
		return tp.UserID, true
	}
	if si, ok := SessionFrom(r.Context()); ok {
		return si.Principal.UserID, true
	}
	return "", false
}

// UploadRateLimit 按用户的上传频率限制中间件(进程内令牌桶)。
// @Description: 须挂在认证中间件(TokenAuth 或 SessionAuth)之后,以认证主体的
// 用户 id 为键——同一用户经 Token 与后台会话上传共享同一额度。
// 额度逐请求取值,热生效:取值 <= 0 表示不启用,额度变更时该用户令牌桶按新额度重建。
// 突发额度等于每分钟额度,满则 429 + Retry-After。
// @param perMinute 每用户每分钟允许上传次数的读取口
// @param l 结构化日志
// @return 中间件
func UploadRateLimit(perMinute func() int, l *slog.Logger) func(http.Handler) http.Handler {
	var mu sync.Mutex
	entries := map[string]*rateEntry{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			quota := perMinute()
			if quota <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			userID, ok := uploadSubject(r)
			if !ok {
				httpx.WriteError(l, w, http.StatusUnauthorized, "未认证的上传请求")
				return
			}
			mu.Lock()
			now := time.Now()
			e, exists := entries[userID]
			if !exists || e.perMinute != quota {
				if !exists && len(entries) >= rateLimiterMaxEntries {
					for k, v := range entries {
						if now.Sub(v.lastSeen) > rateLimiterEntryTTL {
							delete(entries, k)
						}
					}
				}
				e = &rateEntry{
					lim:       rate.NewLimiter(rate.Every(time.Minute/time.Duration(quota)), quota),
					perMinute: quota,
				}
				entries[userID] = e
			}
			e.lastSeen = now
			allowed := e.lim.AllowN(now, 1)
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", "60")
				httpx.WriteError(l, w, http.StatusTooManyRequests, "上传过于频繁,请稍后重试")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
