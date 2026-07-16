package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// 登录限速参数:每个键(IP 或用户名)允许突发 5 次尝试,之后每 12 秒恢复 1 次。
const (
	loginBurst        = 5
	loginRefillEvery  = 12 * time.Second
	limiterMaxEntries = 1024      // 键表容量上限,超出即触发过期清理,防内存无界
	limiterEntryTTL   = time.Hour // 键闲置超过该时长视为可回收
)

// LoginLimiter 进程内登录尝试限速器,按键(IP/用户名)维护独立令牌桶。
type LoginLimiter struct {
	mu      sync.Mutex               // 保护 entries
	entries map[string]*limiterEntry // 键 → 令牌桶
	now     func() time.Time         // 时钟源,测试可注入
}

// limiterEntry 单个键的令牌桶与最近活跃时刻。
type limiterEntry struct {
	lim      *rate.Limiter // 该键的令牌桶
	lastSeen time.Time     // 最近一次尝试时刻,闲置回收依据
}

// NewLoginLimiter 创建登录限速器。
// @return 就绪的限速器
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{entries: map[string]*limiterEntry{}, now: time.Now}
}

// Allow 判定该键的一次登录尝试是否放行,并消耗一个令牌。
// @param key 限速键,调用方以前缀区分维度(如 "ip:1.2.3.4" / "user:admin")
// @return 放行为 true,超速为 false
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= limiterMaxEntries {
			l.prune(now)
		}
		e = &limiterEntry{lim: rate.NewLimiter(rate.Every(loginRefillEvery), loginBurst)}
		l.entries[key] = e
	}
	e.lastSeen = now
	return e.lim.AllowN(now, 1)
}

// Reset 清空指定键的限速状态。
// @Description: 凭证校验成功后调用,限速只惩罚失败尝试——合法用户的正常登录
// 不消耗额度,爆破者的连续失败照常收敛。
// @param key 限速键
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// prune 回收闲置键;须在持锁状态下调用。
// @param now 当前时刻
func (l *LoginLimiter) prune(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.lastSeen) > limiterEntryTTL {
			delete(l.entries, k)
		}
	}
}
