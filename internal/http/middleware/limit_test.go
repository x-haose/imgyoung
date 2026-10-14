package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestConcurrencyLimitRejectsWhenFull 验证并发闸:占满槽位后新请求立即 429,
// 释放后恢复放行。
func TestConcurrencyLimitRejectsWhenFull(t *testing.T) {
	release := make(chan struct{})
	// 缓冲容量覆盖所有会真正进入 handler 的请求(第一与第三),避免测试自身阻塞。
	entered := make(chan struct{}, 2)
	blocked := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})

	handler := ConcurrencyLimit(func() int { return 1 }, slog.New(slog.DiscardHandler))(blocked)

	// 第一个请求占住唯一槽位。
	var wg sync.WaitGroup
	firstRec := httptest.NewRecorder()
	wg.Go(func() {
		handler.ServeHTTP(firstRec, httptest.NewRequest(http.MethodPost, "/", nil))
	})
	<-entered

	// 槽位已满:第二个请求应立即 429。
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("满载时状态码 = %d, 期望 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("429 响应缺少 Retry-After 头")
	}

	// 释放后第三个请求应放行。
	close(release)
	wg.Wait()
	if firstRec.Code != http.StatusOK {
		t.Fatalf("首个请求状态码 = %d, 期望 200", firstRec.Code)
	}
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, httptest.NewRequest(http.MethodPost, "/", nil))
	if third.Code != http.StatusOK {
		t.Fatalf("释放后状态码 = %d, 期望 200", third.Code)
	}
}
