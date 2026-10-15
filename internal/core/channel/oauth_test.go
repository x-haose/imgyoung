package channel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// TestParsePastedTokenAcceptsBothFormats 验证 rclone authorize 的两种输出都被接受。
func TestParsePastedTokenAcceptsBothFormats(t *testing.T) {
	rawToken := `{"access_token":"AT","token_type":"bearer","refresh_token":"RT","expiry":"2030-01-02T15:04:05Z"}`
	encoded := base64.RawStdEncoding.EncodeToString([]byte(`{"token":` + strconvQuote(rawToken) + `}`))

	for name, pasted := range map[string]string{
		"裸令牌 JSON":  rawToken,
		"参数映射编码":    encoded,
		"两侧带空白的裸令牌": "  \n" + rawToken + "\t ",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePastedToken(pasted)
			if err != nil {
				t.Fatalf("ParsePastedToken 报错: %v", err)
			}
			var tok oauth2.Token
			if err := json.Unmarshal([]byte(got), &tok); err != nil {
				t.Fatalf("结果不是合法令牌 JSON: %v", err)
			}
			if tok.AccessToken != "AT" || tok.RefreshToken != "RT" {
				t.Errorf("令牌内容错位: access=%q refresh=%q", tok.AccessToken, tok.RefreshToken)
			}
		})
	}
}

// TestParsePastedTokenAcceptsNonExpiring 验证永不过期的令牌(到期时刻为零值,如 pCloud)不要求刷新令牌。
func TestParsePastedTokenAcceptsNonExpiring(t *testing.T) {
	got, err := ParsePastedToken(`{"access_token":"AT","token_type":"bearer","expiry":"0001-01-01T00:00:00Z"}`)
	if err != nil {
		t.Fatalf("永不过期的令牌被拒: %v", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(got), &tok); err != nil {
		t.Fatalf("结果不是合法令牌 JSON: %v", err)
	}
	if tok.AccessToken != "AT" || !tok.Expiry.IsZero() {
		t.Errorf("令牌内容错位: access=%q expiry=%v", tok.AccessToken, tok.Expiry)
	}
}

// TestParsePastedTokenRejects 验证非法令牌被拒。
// @Description: 会过期却缺刷新令牌必须拒——访问令牌过期后该渠道会静默失效,建时不拦到运行期才炸。
func TestParsePastedTokenRejects(t *testing.T) {
	tests := []struct {
		name   string
		pasted string
	}{
		{"空串", "   "},
		{"非 JSON", "这不是令牌"},
		{"缺访问令牌", `{"refresh_token":"RT"}`},
		{"会过期却缺刷新令牌", `{"access_token":"AT","token_type":"bearer","expiry":"2030-01-02T15:04:05Z"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParsePastedToken(tt.pasted); err == nil {
				t.Fatal("期望报错,实得 nil")
			} else if !errors.Is(err, ErrInvalidChannel) {
				t.Errorf("错误未归入 ErrInvalidChannel: %v", err)
			}
		})
	}
}

// TestOAuthFlowAuthCodeURL 验证授权地址带齐离线授权、自建凭证与回调地址。
func TestOAuthFlowAuthCodeURL(t *testing.T) {
	flow, err := NewOAuthFlow(t.Context(), "drive", map[string]string{
		ConfigKeyClientID:     "my-client",
		ConfigKeyClientSecret: "my-secret",
	}, "https://img.example.com/api/admin/channels/oauth/callback")
	if err != nil {
		t.Fatalf("NewOAuthFlow 报错: %v", err)
	}

	raw := flow.AuthCodeURL("state-123")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("授权地址不是合法 URL: %v", err)
	}
	q := u.Query()
	// 缺离线授权则拿不到刷新令牌,是重启后掉线的根因,必须守住。
	if got := q.Get("access_type"); got != "offline" {
		t.Errorf("access_type = %q, 期望 offline", got)
	}
	if got := q.Get("client_id"); got != "my-client" {
		t.Errorf("client_id = %q, 期望自建值覆盖内置值", got)
	}
	if got := q.Get("redirect_uri"); got != "https://img.example.com/api/admin/channels/oauth/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	if got := q.Get("state"); got != "state-123" {
		t.Errorf("state = %q", got)
	}
}

// TestOAuthFlowAppliesBackendDefaults 验证授权地址用上后端选项的默认值:onedrive 的 scope 取自
// access_scopes 的默认值,缺了它微软不发刷新令牌。
func TestOAuthFlowAppliesBackendDefaults(t *testing.T) {
	flow, err := NewOAuthFlow(t.Context(), "onedrive", map[string]string{
		"region": "global", ConfigKeyClientID: "my-client",
	}, "https://img.example.com/cb")
	if err != nil {
		t.Fatalf("NewOAuthFlow 报错: %v", err)
	}
	u, err := url.Parse(flow.AuthCodeURL("state-123"))
	if err != nil {
		t.Fatalf("授权地址不是合法 URL: %v", err)
	}
	if scope := u.Query().Get("scope"); !strings.Contains(scope, "offline_access") {
		t.Errorf("scope = %q, 期望含 offline_access", scope)
	}
}

// TestTemplateEditKeepsAuthorizationParams 验证编辑 OneDrive 模板渠道而不重新授权时,授权带出的
// drive_id / drive_type 随授权保留,模板字段照常替换;缺了它们后端无法构造。
func TestTemplateEditKeepsAuthorizationParams(t *testing.T) {
	stored := map[string]string{
		"region": "global", ConfigKeyToken: `{"access_token":"AT"}`, "path": "old",
		"drive_id": "DRIVE-A", "drive_type": "personal",
	}
	drv, submitted, err := BuildTemplateConfig("onedrive", map[string]string{
		"region": "global", ConfigKeyToken: SecretPlaceholder, "path": "new",
	})
	if err != nil {
		t.Fatalf("合并模板配置: %v", err)
	}
	config, err := prepareConfig(drv, submitted, stored, true)
	if err != nil {
		t.Fatalf("整理配置: %v", err)
	}
	if err := completeTemplateConfig(t.Context(), "onedrive", submitted, config, stored); err != nil {
		t.Fatalf("补齐授权参数: %v", err)
	}
	if config["drive_id"] != "DRIVE-A" || config["drive_type"] != "personal" {
		t.Errorf("授权带出的参数未保留: %v", config)
	}
	if config["path"] != "new" || config[ConfigKeyToken] != stored[ConfigKeyToken] {
		t.Errorf("模板字段未替换或令牌未沿用: %v", config)
	}
}

// TestOAuthFlowExchange 验证授权码换取令牌并归一化为 rclone 存储格式。
func TestOAuthFlowExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		if got := r.Form.Get("code"); got != "the-code" {
			t.Errorf("授权码 = %q, 期望 the-code", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"新AT","token_type":"bearer",` +
			`"refresh_token":"新RT","expires_in":3600}`))
	}))
	defer srv.Close()

	// token_url 覆盖是 rclone 既有语义,借它把端点指向测试服务器。
	flow, err := NewOAuthFlow(t.Context(), "drive", map[string]string{
		ConfigKeyClientID: "my-client",
		"token_url":       srv.URL,
	}, "https://img.example.com/cb")
	if err != nil {
		t.Fatalf("NewOAuthFlow 报错: %v", err)
	}

	got, err := flow.Exchange(t.Context(), "the-code")
	if err != nil {
		t.Fatalf("Exchange 报错: %v", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(got), &tok); err != nil {
		t.Fatalf("结果不是合法令牌 JSON: %v", err)
	}
	if tok.AccessToken != "新AT" || tok.RefreshToken != "新RT" {
		t.Errorf("令牌内容错位: access=%q refresh=%q", tok.AccessToken, tok.RefreshToken)
	}
	if tok.Expiry.IsZero() {
		t.Error("到期时刻为零值,刷新判定会失准")
	}
}

// TestNewOAuthFlowRejectsNonOAuthDriver 验证免授权后端不会误入授权流:
// s3 有交互式配置但不走 OAuth,webdav 与 local 根本没有配置流程。
func TestNewOAuthFlowRejectsNonOAuthDriver(t *testing.T) {
	for _, driver := range []string{"s3", "webdav", "local"} {
		t.Run(driver, func(t *testing.T) {
			if _, err := NewOAuthFlow(t.Context(), driver, nil, "https://img.example.com/cb"); err == nil {
				t.Fatal("期望报错,实得 nil")
			} else if !errors.Is(err, ErrNotOAuthDriver) {
				t.Errorf("错误未归入 ErrNotOAuthDriver: %v", err)
			}
		})
	}
	if _, err := NewOAuthFlow(t.Context(), "没有这个后端", nil, ""); !errors.Is(err, ErrInvalidChannel) {
		t.Errorf("未知后端错误 = %v, 期望归入 ErrInvalidChannel", err)
	}
}

// TestNewOAuthFlowWalksToOAuthState 验证授权环节不在首步的后端(Google Drive 先查
// client_id)也能走到授权选项;缺 client_id 时后端要求交互输入,须以明确错误拒绝。
func TestNewOAuthFlowWalksToOAuthState(t *testing.T) {
	if _, err := NewOAuthFlow(t.Context(), "drive", map[string]string{ConfigKeyClientID: "cid"}, ""); err != nil {
		t.Fatalf("带 client_id 的 drive 应走到授权环节: %v", err)
	}
	_, err := NewOAuthFlow(t.Context(), "drive", nil, "")
	if !errors.Is(err, ErrInvalidChannel) || errors.Is(err, ErrNotOAuthDriver) {
		t.Fatalf("缺 client_id 的 drive 错误 = %v, 期望指出需要先提供参数的 ErrInvalidChannel", err)
	}
}

// strconvQuote 以 JSON 语义给字符串加引号转义。
func strconvQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
