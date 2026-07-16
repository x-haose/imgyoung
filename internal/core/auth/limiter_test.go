package auth

import (
	"fmt"
	"testing"
	"time"
)

// TestLimiterBurstAndRefill 突发额度耗尽后拒绝,等待补充间隔后恢复,键之间互不影响。
func TestLimiterBurstAndRefill(t *testing.T) {
	now := time.Now()
	l := NewLoginLimiter()
	l.now = func() time.Time { return now }

	for i := range loginBurst {
		if !l.Allow("ip:a") {
			t.Fatalf("第 %d 次尝试应在突发额度内放行", i+1)
		}
	}
	if l.Allow("ip:a") {
		t.Fatal("突发额度耗尽后应拒绝")
	}
	if !l.Allow("ip:b") {
		t.Fatal("其他键不应受影响")
	}
	now = now.Add(loginRefillEvery + time.Second)
	if !l.Allow("ip:a") {
		t.Fatal("补充间隔后应恢复一次额度")
	}
}

// TestLimiterPrune 键表达到容量上限时回收闲置键,容量有界。
func TestLimiterPrune(t *testing.T) {
	now := time.Now()
	l := NewLoginLimiter()
	l.now = func() time.Time { return now }
	for i := range limiterMaxEntries {
		l.Allow(fmt.Sprintf("ip:%d", i))
	}
	now = now.Add(limiterEntryTTL + time.Minute)
	l.Allow("ip:new")
	if len(l.entries) != 1 {
		t.Fatalf("闲置键应被回收, 剩余 %d", len(l.entries))
	}
}
