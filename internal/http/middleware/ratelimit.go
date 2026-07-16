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
	lim      *rate.Limiter // 该用户的令牌桶
	lastSeen time.Time     // 最近一次请求时刻,闲置回收依据
}

// UploadRateLimit 按用户的上传频率限制中间件(进程内令牌桶)。
// @Description: 须挂在 TokenAuth 之后,以认证主体的用户 id 为键;
// perMinute <= 0 表示不启用。突发额度等于每分钟额度,满则 429 + Retry-After。
// @param perMinute 每用户每分钟允许的上传次数
// @param l 结构化日志
// @return 中间件
func UploadRateLimit(perMinute int, l *slog.Logger) func(http.Handler) http.Handler {
	if perMinute <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	var mu sync.Mutex
	entries := map[string]*rateEntry{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tp, ok := TokenPrincipalFrom(r.Context())
			if !ok {
				httpx.WriteError(l, w, http.StatusUnauthorized, "无效的上传 Token")
				return
			}
			mu.Lock()
			now := time.Now()
			e, exists := entries[tp.UserID]
			if !exists {
				if len(entries) >= rateLimiterMaxEntries {
					for k, v := range entries {
						if now.Sub(v.lastSeen) > rateLimiterEntryTTL {
							delete(entries, k)
						}
					}
				}
				e = &rateEntry{lim: rate.NewLimiter(rate.Every(time.Minute/time.Duration(perMinute)), perMinute)}
				entries[tp.UserID] = e
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
