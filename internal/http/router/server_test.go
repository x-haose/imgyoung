package router_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/referer"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/http/router"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// testToken 测试用上传 Token 明文;newTestServer 以其 SHA256 直插一行有效 Token。
const testToken = "test-token"

// testAdminPass 测试用管理员密码。
const testAdminPass = "test-admin-pass"

// testEnv 测试服务的底层构件,供 mutate 回调按用例定制(如构建 variants 服务)。
type testEnv struct {
	Dir     string           // 临时数据目录
	Conn    *sql.DB          // 数据库连接
	Queries *sqlc.Queries    // sqlc 查询集
	Mgr     *channel.Manager // 渠道管理器
	Store   *config.Store    // 运营配置存储,applySettings 据此热改
}

// testConfig 三面配置的聚合,mutate 回调据此按用例定制。
type testConfig struct {
	Public publicapi.Config // 数据面配置
	Admin  adminapi.Config  // 管理面配置
	Router router.Config    // 路由级配置(Public/Admin 字段由助手最终填充)
}

// clearTestEnv 置空当前环境里全部 IMGYOUNG_ 变量,防止开发机残留影响配置播种。
func clearTestEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "IMGYOUNG_") {
			t.Setenv(k, "")
		}
	}
}

// applySettings 经存储热改运营配置:当前快照上覆写后整体写回,立即生效。
func applySettings(t *testing.T, store *config.Store, mod func(*config.Settings)) {
	t.Helper()
	set := store.Current()
	mod(&set)
	blob, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("编码配置: %v", err)
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(blob, &patch); err != nil {
		t.Fatalf("展开配置: %v", err)
	}
	if _, err := store.Update(t.Context(), patch); err != nil {
		t.Fatalf("应用配置: %v", err)
	}
}

// newTestServer 组装一个使用临时目录(DB + local 渠道)的完整测试服务。
// @param mutate 可选的配置修改器,用于按用例定制防盗链/BaseURL/按需处理等
func newTestServer(t *testing.T, mutate func(*testConfig, testEnv)) *httptest.Server {
	t.Helper()
	clearTestEnv(t)
	env := testenv.New(t)
	store, err := config.LoadStore(t.Context(), env.Conn, env.Queries, env.Master)
	if err != nil {
		t.Fatalf("装载运营配置: %v", err)
	}
	// 测试用小额度:单图 1MB、像素预算 1MP,便于构造超限用例。
	applySettings(t, store, func(s *config.Settings) {
		s.MaxUploadMB = 1
		s.MaxPixelsMP = 1
	})
	svc := upload.New(upload.Config{
		Conn:     env.Conn,
		Queries:  env.Queries,
		Channels: env.Manager,
		Settings: store.Current,
		Logger:   env.Logger,
	})
	// 登录限速用冻结时钟:每次失败尝试都要付一遍 argon2id 的代价(竞态检测下更慢),走墙钟时
	// 额度会在尝试之间恢复,限速断言随机化;冻结后只取决于尝试次数。
	frozen := time.Now()
	authSvc := auth.New(auth.Config{
		Conn: env.Conn, Queries: env.Queries, Logger: env.Logger, Now: func() time.Time { return frozen },
	})
	sum := sha256.Sum256([]byte(testToken))
	if _, err := env.Queries.CreateToken(t.Context(), sqlc.CreateTokenParams{
		UserID: sqlc.DefaultUserID, Name: "test", TokenHash: hex.EncodeToString(sum[:]),
		Prefix: testToken, Scopes: "upload",
	}); err != nil {
		t.Fatalf("植入测试 Token: %v", err)
	}
	if err := authSvc.SetPassword(t.Context(), sqlc.DefaultUserID, testAdminPass); err != nil {
		t.Fatalf("设置管理员密码: %v", err)
	}
	refererSvc, err := referer.New(t.Context(), env.Queries)
	if err != nil {
		t.Fatalf("创建防盗链服务: %v", err)
	}

	boot, err := config.LoadBootstrap()
	if err != nil {
		t.Fatalf("装配引导配置: %v", err)
	}
	tc := testConfig{
		Public: publicapi.Config{
			Queries:  env.Queries,
			Channels: env.Manager,
			Upload:   svc,
			Settings: store.Current,
			Activity: lifecycle.NewTracker(env.Conn, env.Queries, func() bool { return false }, env.Logger),
			Logger:   env.Logger,
		},
		Admin: adminapi.Config{
			Admin:    env.Manager,
			Auth:     authSvc,
			Queries:  env.Queries,
			Audit:    audit.New(env.Queries),
			Referer:  refererSvc,
			Settings: store,
			Boot:     boot,
			Logger:   env.Logger,
		},
		Router: router.Config{
			Auth:            authSvc,
			Settings:        store.Current,
			RefererPatterns: refererSvc.Patterns,
			Metrics:         observability.New(),
			TrustedProxies:  boot.TrustedProxies,
			MetricsAllow:    boot.MetricsAllow,
			Logger:          env.Logger,
		},
	}
	if mutate != nil {
		mutate(&tc, testEnv{
			Dir: env.DataDir, Conn: env.Conn, Queries: env.Queries, Mgr: env.Manager, Store: store,
		})
	}
	tc.Router.Public = publicapi.New(tc.Public)
	tc.Router.Admin = adminapi.New(tc.Admin)
	ts := httptest.NewServer(router.New(tc.Router))
	t.Cleanup(ts.Close)
	return ts
}

// pngBytes 生成一张 2x2 的真实 PNG 图片字节。
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// multipartBody 构造带单个 file 字段的 multipart 请求体。
// @return 请求体与对应的 Content-Type
func multipartBody(t *testing.T, filename string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("创建 multipart 字段: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("写入 multipart 内容: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// uploadReq 以测试 Token 向测试服务发起一次上传请求;extraHeaders 追加请求头。
func uploadReq(t *testing.T, ts *httptest.Server, filename string, payload []byte,
	extraHeaders map[string]string) *http.Response {
	t.Helper()
	body, contentType := multipartBody(t, filename, payload)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload", body)
	if err != nil {
		t.Fatalf("构造上传请求: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("发送上传请求: %v", err)
	}
	return resp
}

// closeBody 关闭响应体并在失败时记录测试错误。
func closeBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("关闭响应体: %v", err)
	}
}

// decodeUpload 解析上传响应体;url 按契约应为绝对地址(未配 base_url 时按请求
// 域名推导),校验后返回其路径部分供测试拼接访问。
func decodeUpload(t *testing.T, resp *http.Response) (id, path string) {
	t.Helper()
	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	u, err := neturl.Parse(result.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		t.Fatalf("上传响应 url = %q, 期望为绝对地址", result.URL)
	}
	return result.ID, u.Path
}

// rawUploadURL 解码上传响应并返回未加工的 url 字段(站点地址断言用)。
func rawUploadURL(t *testing.T, resp *http.Response) string {
	t.Helper()
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	return result.URL
}

// adminSession 登录管理员,返回带会话 cookie 的客户端与 CSRF 令牌。
func adminSession(t *testing.T, ts *httptest.Server) (*http.Client, string) {
	t.Helper()
	return loginAs(t, ts, "admin", testAdminPass)
}

// loginAs 以指定用户登录,返回带会话 cookie 的客户端与 CSRF 令牌。
func loginAs(t *testing.T, ts *httptest.Server, name, password string) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	body := fmt.Sprintf(`{"name":%q,"password":%q}`, name, password)
	resp, err := client.Post(ts.URL+"/api/admin/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("发送登录请求: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析登录响应: %v", err)
	}
	return client, lr.CSRFToken
}

// adminReq 以管理员会话发送请求(状态变更自动带 CSRF 头)。
func adminReq(t *testing.T, client *http.Client, csrf, method, url, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("构造管理请求: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发送管理请求: %v", err)
	}
	return resp
}
