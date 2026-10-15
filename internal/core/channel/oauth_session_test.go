package channel

import (
	"errors"
	"testing"
	"time"
)

// TestOAuthSessionRoundTrip 验证登记、回读、完成、领取的正常闭环。
func TestOAuthSessionRoundTrip(t *testing.T) {
	s := NewOAuthSessions()
	state, err := s.Start("drive", map[string]string{"client_id": "cid"}, "https://x/cb")
	if err != nil {
		t.Fatalf("Start 报错: %v", err)
	}

	pending, err := s.Pending(state)
	if err != nil {
		t.Fatalf("Pending 报错: %v", err)
	}
	if pending.Driver != "drive" || pending.Params["client_id"] != "cid" || pending.RedirectURI != "https://x/cb" {
		t.Errorf("回读错位: %+v", pending)
	}

	// 未完成时领取应得空结果而非报错——前端靠这个状态继续轮询。
	tok, failure, err := s.Take(state)
	if err != nil || tok != "" || failure != nil {
		t.Errorf("未完成时 Take = (%q, %v, %v), 期望全空", tok, failure, err)
	}

	if err := s.Complete(state, `{"access_token":"AT"}`, nil); err != nil {
		t.Fatalf("Complete 报错: %v", err)
	}
	tok, failure, err = s.Take(state)
	if err != nil || tok != `{"access_token":"AT"}` || failure != nil {
		t.Errorf("Take = (%q, %v, %v)", tok, failure, err)
	}
}

// TestOAuthSessionTakeIsOneShot 验证结果只能领取一次,同一 state 不可重放。
func TestOAuthSessionTakeIsOneShot(t *testing.T) {
	s := NewOAuthSessions()
	state, err := s.Start("drive", nil, "https://x/cb")
	if err != nil {
		t.Fatalf("Start 报错: %v", err)
	}
	if err := s.Complete(state, "TOKEN", nil); err != nil {
		t.Fatalf("Complete 报错: %v", err)
	}
	if _, _, err := s.Take(state); err != nil {
		t.Fatalf("首次 Take 报错: %v", err)
	}
	if _, _, err := s.Take(state); !errors.Is(err, ErrOAuthSessionNotFound) {
		t.Errorf("二次 Take 错误 = %v, 期望 ErrOAuthSessionNotFound", err)
	}
}

// TestOAuthSessionParamsAreCopied 验证登记后改动原参数表不影响会话。
func TestOAuthSessionParamsAreCopied(t *testing.T) {
	s := NewOAuthSessions()
	params := map[string]string{"client_id": "cid"}
	state, err := s.Start("drive", params, "https://x/cb")
	if err != nil {
		t.Fatalf("Start 报错: %v", err)
	}
	params["client_id"] = "被改掉了"

	again, err := s.Pending(state)
	if err != nil {
		t.Fatalf("Pending 报错: %v", err)
	}
	if again.Params["client_id"] != "cid" {
		t.Errorf("会话参数被外部改动污染: %q", again.Params["client_id"])
	}
}

// TestOAuthSessionExpires 验证过期会话不可用。
func TestOAuthSessionExpires(t *testing.T) {
	s := NewOAuthSessions()
	state, err := s.Start("drive", nil, "https://x/cb")
	if err != nil {
		t.Fatalf("Start 报错: %v", err)
	}
	// 直接回拨创建时刻,避免测试等待真实 TTL。
	s.mu.Lock()
	s.sessions[state].createdAt = time.Now().Add(-oauthSessionTTL - time.Second)
	s.mu.Unlock()

	if _, err := s.Pending(state); !errors.Is(err, ErrOAuthSessionNotFound) {
		t.Errorf("Pending 错误 = %v, 期望 ErrOAuthSessionNotFound", err)
	}
	if err := s.Complete(state, "TOKEN", nil); !errors.Is(err, ErrOAuthSessionNotFound) {
		t.Errorf("Complete 错误 = %v, 期望 ErrOAuthSessionNotFound", err)
	}
}

// TestOAuthSessionUnknownState 验证未知 state 一律被拒。
func TestOAuthSessionUnknownState(t *testing.T) {
	s := NewOAuthSessions()
	if _, err := s.Pending("没这个"); !errors.Is(err, ErrOAuthSessionNotFound) {
		t.Errorf("Pending 错误 = %v", err)
	}
	if _, _, err := s.Take("没这个"); !errors.Is(err, ErrOAuthSessionNotFound) {
		t.Errorf("Take 错误 = %v", err)
	}
}
