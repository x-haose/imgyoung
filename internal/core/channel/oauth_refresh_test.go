package channel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// TestNeedsRefresh 验证续期判定的各分支。
func TestNeedsRefresh(t *testing.T) {
	tests := []struct {
		name string
		tok  oauth2.Token
		want bool
	}{
		{"临期应刷新", oauth2.Token{RefreshToken: "RT", Expiry: time.Now().Add(5 * time.Minute)}, true},
		{"已过期应刷新", oauth2.Token{RefreshToken: "RT", Expiry: time.Now().Add(-time.Hour)}, true},
		{"寿命充裕不刷新", oauth2.Token{RefreshToken: "RT", Expiry: time.Now().Add(6 * time.Hour)}, false},
		// 到期时刻缺失多为长期令牌,盲目刷新只会平添失败面。
		{"无到期时刻不刷新", oauth2.Token{RefreshToken: "RT"}, false},
		// 无刷新令牌时无从续期,提前判掉避免必然失败的请求。
		{"无刷新令牌不刷新", oauth2.Token{Expiry: time.Now().Add(time.Minute)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsRefresh(&tt.tok); got != tt.want {
				t.Errorf("needsRefresh = %v, 期望 %v", got, tt.want)
			}
		})
	}
}

// TestOAuthFlowRefresh 验证刷新换回新令牌。
func TestOAuthFlowRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, 期望 refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "旧RT" {
			t.Errorf("refresh_token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"新AT","token_type":"bearer",` +
			`"refresh_token":"新RT","expires_in":3600}`))
	}))
	defer srv.Close()

	flow, err := NewOAuthFlow(t.Context(), "drive", map[string]string{
		ConfigKeyClientID: "cid", "token_url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("NewOAuthFlow 报错: %v", err)
	}

	fresh, err := flow.Refresh(t.Context(), &oauth2.Token{
		AccessToken: "旧AT", RefreshToken: "旧RT", Expiry: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Refresh 报错: %v", err)
	}
	if fresh.AccessToken != "新AT" {
		t.Errorf("访问令牌 = %q, 期望 新AT", fresh.AccessToken)
	}
	// 服务商轮换刷新令牌时必须接住新值,否则下轮续期还用旧值、最终掉线。
	if fresh.RefreshToken != "新RT" {
		t.Errorf("刷新令牌 = %q, 期望接住轮换后的新值", fresh.RefreshToken)
	}

	// 序列化后须能被 rclone 原样读回。
	raw, err := marshalToken(fresh)
	if err != nil {
		t.Fatalf("marshalToken 报错: %v", err)
	}
	var back oauth2.Token
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if back.RefreshToken != "新RT" {
		t.Errorf("回读刷新令牌 = %q", back.RefreshToken)
	}
}
