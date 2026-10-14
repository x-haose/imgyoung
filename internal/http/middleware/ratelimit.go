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

// rateLimiters 按用户 id 索引的令牌桶表;表满时回收闲置键,内存有界。
type rateLimiters struct {
	mu      sync.Mutex
	entries map[string]*rateEntry // 用户 id → 令牌桶
}

// allow 报告用户此刻是否还有上传额度,并消耗一个令牌。
// @Description: 用户首次出现或额度变更时按 quota 新建令牌桶(突发额度等于每分钟额度)。
// @param userID 认证主体用户 id
// @param quota 当前每分钟额度,须 > 0
// @param now 判定时刻
// @return 是否放行
func (t *rateLimiters) allow(userID string, quota int, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, exists := t.entries[userID]
	if !exists || e.perMinute != quota {
		if !exists {
			t.evictIdle(now)
		}
		e = &rateEntry{
			lim:       rate.NewLimiter(rate.Every(time.Minute/time.Duration(quota)), quota),
			perMinute: quota,
		}
		t.entries[userID] = e
	}
	e.lastSeen = now
	return e.lim.AllowN(now, 1)
}

// evictIdle 表达到容量上限时删除闲置超过 rateLimiterEntryTTL 的键;未满则不动。
// @param now 判定时刻
func (t *rateLimiters) evictIdle(now time.Time) {
	if len(t.entries) < rateLimiterMaxEntries {
		return
	}
	for k, v := range t.entries {
		if now.Sub(v.lastSeen) > rateLimiterEntryTTL {
			delete(t.entries, k)
		}
	}
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
	table := &rateLimiters{entries: map[string]*rateEntry{}}
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
			if !table.allow(userID, quota, time.Now()) {
				w.Header().Set("Retry-After", "60")
				httpx.WriteError(l, w, http.StatusTooManyRequests, "上传过于频繁,请稍后重试")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
