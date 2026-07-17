package router_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/http/adminapi"
	"github.com/x-haose/imgyoung/internal/http/publicapi"
	"github.com/x-haose/imgyoung/internal/http/router"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/observability"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// testToken 测试用上传 Token(经 bootstrap 植入)。
const testToken = "test-token"

// testAdminPass 测试用管理员密码。
const testAdminPass = "test-admin-pass"

// testEnv 测试服务的底层构件,供 mutate 回调按用例定制(如构建 variants 服务)。
type testEnv struct {
	Dir     string           // 临时数据目录
	Conn    *sql.DB          // 数据库连接
	Queries *sqlc.Queries    // sqlc 查询集
	Mgr     *channel.Manager // 渠道管理器
}

// testConfig 三面配置的聚合,mutate 回调据此按用例定制。
type testConfig struct {
	Public publicapi.Config // 数据面配置
	Admin  adminapi.Config  // 管理面配置
	Router router.Config    // 路由级配置(Public/Admin 字段由助手最终填充)
}

// newTestServer 组装一个使用临时目录(DB + local 渠道)的完整测试服务。
// @param mutate 可选的配置修改器,用于按用例定制防盗链/BaseURL/按需处理等
func newTestServer(t *testing.T, mutate func(*testConfig, testEnv)) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	discard := slog.New(slog.DiscardHandler)

	conn, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() {
		if cerr := conn.Close(); cerr != nil {
			t.Errorf("关闭测试数据库: %v", cerr)
		}
	})
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}

	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	queries := sqlc.New(conn)
	mgr, err := channel.NewManager(t.Context(), channel.ManagerConfig{
		Conn:    conn,
		Queries: queries,
		Master:  master,
		DataDir: dir,
		Logger:  discard,
	})
	if err != nil {
		t.Fatalf("创建渠道管理器: %v", err)
	}
	svc := upload.New(upload.Config{
		Conn:      conn,
		Queries:   queries,
		Channels:  mgr,
		MaxPixels: 1 << 20,
		Logger:    discard,
	})
	authSvc := auth.New(auth.Config{Conn: conn, Queries: queries, Logger: discard})
	if err := authSvc.EnsureBootstrapToken(t.Context(), sqlc.DefaultUserID, testToken); err != nil {
		t.Fatalf("植入测试 Token: %v", err)
	}
	if err := authSvc.SetPassword(t.Context(), sqlc.DefaultUserID, testAdminPass); err != nil {
		t.Fatalf("设置管理员密码: %v", err)
	}

	tc := testConfig{
		Public: publicapi.Config{
			Queries:           queries,
			Channels:          mgr,
			Upload:            svc,
			MaxUploadBytes:    1 << 20,
			DefaultEXIFPolicy: imaging.PolicyPassthrough,
			Activity:          lifecycle.NewTracker(conn, queries, discard),
			Logger:            discard,
		},
		Admin: adminapi.Config{
			Admin:  mgr,
			Auth:   authSvc,
			Audit:  audit.New(queries),
			Logger: discard,
		},
		Router: router.Config{
			Auth:                 authSvc,
			MaxConcurrentUploads: 4,
			AllowEmptyReferer:    true,
			Metrics:              observability.New(),
			Logger:               discard,
		},
	}
	if mutate != nil {
		mutate(&tc, testEnv{Dir: dir, Conn: conn, Queries: queries, Mgr: mgr})
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

// uploadReq 向测试服务发起一次上传请求;extraHeaders 追加请求头。
func uploadReq(t *testing.T, ts *httptest.Server, token, filename string, payload []byte, extraHeaders map[string]string) *http.Response {
	t.Helper()
	body, contentType := multipartBody(t, filename, payload)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload", body)
	if err != nil {
		t.Fatalf("构造上传请求: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

// decodeUpload 解析上传响应体。
func decodeUpload(t *testing.T, resp *http.Response) (id, url string) {
	t.Helper()
	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	return result.ID, result.URL
}

// TestUploadRequiresAuth 验证缺失或错误 Token 一律 401。
func TestUploadRequiresAuth(t *testing.T) {
	ts := newTestServer(t, nil)
	for name, token := range map[string]string{"无Token": "", "错误Token": "wrong"} {
		t.Run(name, func(t *testing.T) {
			resp := uploadReq(t, ts, token, "a.png", pngBytes(t), nil)
			defer closeBody(t, resp)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("状态码 = %d, 期望 401", resp.StatusCode)
			}
		})
	}
}

// TestUploadRejectsNonImage 验证非图片内容(按 magic bytes 判定)返回 415。
func TestUploadRejectsNonImage(t *testing.T) {
	ts := newTestServer(t, nil)
	resp := uploadReq(t, ts, testToken, "fake.png", []byte("plain text disguised as png"), nil)
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("状态码 = %d, 期望 415", resp.StatusCode)
	}
}

// TestUploadAndServeRoundTrip 验证上传后能按短 ID 取回,且响应头正确。
// @Description: JPEG 会被净化改字节,故用 PNG 验证字节一致性。
func TestUploadAndServeRoundTrip(t *testing.T) {
	ts := newTestServer(t, nil)
	payload := pngBytes(t)

	resp := uploadReq(t, ts, testToken, "a.png", payload, nil)
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("上传状态码 = %d, 期望 200", resp.StatusCode)
	}
	imgID, imgURL := decodeUpload(t, resp)
	if imgID == "" || !strings.HasPrefix(imgURL, "/i/") {
		t.Fatalf("上传响应异常: id=%q url=%q", imgID, imgURL)
	}

	get, err := ts.Client().Get(ts.URL + imgURL)
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer closeBody(t, get)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("访问状态码 = %d, 期望 200", get.StatusCode)
	}
	if ct := get.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, 期望 image/png", ct)
	}
	if v := get.Header.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, 期望 nosniff", v)
	}
	got, err := io.ReadAll(get.Body)
	if err != nil {
		t.Fatalf("读取图片响应: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("取回字节与上传不一致: got %d bytes, want %d bytes", len(got), len(payload))
	}
}

// TestUploadIdempotencyHeader 验证携带同一 Idempotency-Key 的重复上传返回同一 ID。
func TestUploadIdempotencyHeader(t *testing.T) {
	ts := newTestServer(t, nil)
	payload := pngBytes(t)
	headers := map[string]string{"Idempotency-Key": "client-retry-1"}

	first := uploadReq(t, ts, testToken, "a.png", payload, headers)
	defer closeBody(t, first)
	firstID, _ := decodeUpload(t, first)

	second := uploadReq(t, ts, testToken, "a.png", payload, headers)
	defer closeBody(t, second)
	secondID, _ := decodeUpload(t, second)

	if firstID == "" || firstID != secondID {
		t.Fatalf("幂等键失效: %q ≠ %q", firstID, secondID)
	}
}

// TestUploadEXIFPolicyField 验证 multipart 表单字段 exif_policy 可覆盖单次上传档位,
// 非法取值返回 400。
func TestUploadEXIFPolicyField(t *testing.T) {
	ts := newTestServer(t, nil)

	post := func(policy string) *http.Response {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if policy != "" {
			if err := w.WriteField("exif_policy", policy); err != nil {
				t.Fatalf("写入表单字段: %v", err)
			}
		}
		fw, err := w.CreateFormFile("file", "a.png")
		if err != nil {
			t.Fatalf("创建 multipart 字段: %v", err)
		}
		if _, err := fw.Write(pngBytes(t)); err != nil {
			t.Fatalf("写入 multipart 内容: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("关闭 multipart writer: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload", &buf)
		if err != nil {
			t.Fatalf("构造上传请求: %v", err)
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("发送上传请求: %v", err)
		}
		return resp
	}

	ok := post("strip")
	defer closeBody(t, ok)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("strip 上传状态码 = %d, 期望 200", ok.StatusCode)
	}
	byDefault := post("")
	defer closeBody(t, byDefault)
	if byDefault.StatusCode != http.StatusOK {
		t.Fatalf("未指定档位(用全局默认)状态码 = %d, 期望 200", byDefault.StatusCode)
	}
	bad := post("spoof")
	defer closeBody(t, bad)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("spoof 上传状态码 = %d, 期望 400(实验特性未实现)", bad.StatusCode)
	}
}

// TestUploadBaseURL 验证配置 BaseURL 后响应返回绝对地址(PicGo 等客户端需要)。
func TestUploadBaseURL(t *testing.T) {
	ts := newTestServer(t, func(tc *testConfig, _ testEnv) { tc.Public.BaseURL = "https://img.example.com" })
	resp := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, resp)
	_, imgURL := decodeUpload(t, resp)
	if !strings.HasPrefix(imgURL, "https://img.example.com/i/") {
		t.Fatalf("url = %q, 期望以 BaseURL 开头的绝对地址", imgURL)
	}
}

// TestRefererProtection 验证防盗链:白名单命中放行、未命中 403、空 Referer 按配置。
func TestRefererProtection(t *testing.T) {
	ts := newTestServer(t, func(tc *testConfig, _ testEnv) {
		tc.Router.RefererWhitelist = []string{"*.ok.com"}
		tc.Router.AllowEmptyReferer = true
	})
	resp := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, resp)
	_, imgURL := decodeUpload(t, resp)

	get := func(referer string) int {
		req, err := http.NewRequest(http.MethodGet, ts.URL+imgURL, nil)
		if err != nil {
			t.Fatalf("构造访问请求: %v", err)
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		r, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("访问图片: %v", err)
		}
		defer closeBody(t, r)
		return r.StatusCode
	}

	if code := get(""); code != http.StatusOK {
		t.Errorf("空 Referer 状态码 = %d, 期望 200(默认放行)", code)
	}
	if code := get("https://a.ok.com/post/1"); code != http.StatusOK {
		t.Errorf("白名单 Referer 状态码 = %d, 期望 200", code)
	}
	if code := get("https://evil.io/steal"); code != http.StatusForbidden {
		t.Errorf("盗链 Referer 状态码 = %d, 期望 403", code)
	}
}

// TestUploadTooLarge 验证超过单图上限的上传返回 413(内存闸门)。
func TestUploadTooLarge(t *testing.T) {
	ts := newTestServer(t, nil)
	// 测试服务上限为 1MB,构造 2MB 载荷(以 PNG 头开头以排除 415 干扰)。
	payload := append(pngBytes(t), bytes.Repeat([]byte{0}, 2<<20)...)
	resp := uploadReq(t, ts, testToken, "big.png", payload, nil)
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, 期望 413", resp.StatusCode)
	}
}

// TestServeUnknownID 验证访问不存在的短 ID 返回 404。
func TestServeUnknownID(t *testing.T) {
	ts := newTestServer(t, nil)
	resp, err := ts.Client().Get(ts.URL + "/i/nonexistent0")
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", resp.StatusCode)
	}
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

// postChannel 通过管理 API 创建渠道(自动登录管理员)。
// @return 响应对象(调用方负责关闭与断言)
func postChannel(t *testing.T, ts *httptest.Server, body string) *http.Response {
	t.Helper()
	client, csrf := adminSession(t, ts)
	return adminReq(t, client, csrf, http.MethodPost, ts.URL+"/api/admin/channels", body)
}

// TestChannelCRUD 验证渠道管理 API:创建、列表、删除与唯一副本守卫。
func TestChannelCRUD(t *testing.T) {
	ts := newTestServer(t, nil)
	client, csrf := adminSession(t, ts)

	// 创建第二个渠道(低优先级,不参与上传主写)。
	body := fmt.Sprintf(`{"name":"backup","driver":"local","priority":10,"config":{"path":%q}}`, t.TempDir())
	created := adminReq(t, client, csrf, http.MethodPost, ts.URL+"/api/admin/channels", body)
	defer closeBody(t, created)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("创建渠道状态码 = %d, 期望 201", created.StatusCode)
	}
	var cr struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&cr); err != nil {
		t.Fatalf("解析创建响应: %v", err)
	}

	// 列表须走 {items,total} 信封(与前端 dataProvider 契约一致)、含两个渠道且不泄露配置。
	listResp := adminReq(t, client, csrf, http.MethodGet, ts.URL+"/api/admin/channels", "")
	defer closeBody(t, listResp)
	raw, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("读取列表响应: %v", err)
	}
	var lr struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("列表响应非 {items,total} 信封: %v; body=%s", err, raw)
	}
	hasBackup := false
	for _, c := range lr.Items {
		if c.Name == "backup" {
			hasBackup = true
		}
	}
	if lr.Total < 2 || !hasBackup {
		t.Fatalf("渠道列表信封异常(应含 backup 且 total≥2): %+v", lr)
	}
	if bytes.Contains(raw, []byte("config")) {
		t.Fatalf("渠道列表泄露配置字段: %s", raw)
	}

	// 上传一张图:唯一副本在主渠道(内置 local),删除它应被守卫拒绝。
	up := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, up)
	localID := int64(1) // 迁移内置渠道为首条记录
	del := func(id int64) int {
		dresp := adminReq(t, client, csrf, http.MethodDelete, fmt.Sprintf("%s/api/admin/channels/%d", ts.URL, id), "")
		defer closeBody(t, dresp)
		return dresp.StatusCode
	}
	if code := del(localID); code != http.StatusConflict {
		t.Fatalf("删除持有唯一副本的渠道状态码 = %d, 期望 409", code)
	}
	if code := del(cr.ID); code != http.StatusNoContent {
		t.Fatalf("删除空渠道状态码 = %d, 期望 204", code)
	}
}

// TestAdminAuthFlow 验证管理面认证闭环:错误密码 401、登录成功、/me 恢复会话、
// 无 CSRF 头的状态变更 403、登出后会话失效。
func TestAdminAuthFlow(t *testing.T) {
	ts := newTestServer(t, nil)

	// 错误密码 401。
	bad, err := http.Post(ts.URL+"/api/admin/auth/login", "application/json",
		strings.NewReader(`{"name":"admin","password":"wrong"}`))
	if err != nil {
		t.Fatalf("发送登录请求: %v", err)
	}
	defer closeBody(t, bad)
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误密码状态码 = %d, 期望 401", bad.StatusCode)
	}

	client, csrf := adminSession(t, ts)

	// /me 返回主体与令牌。
	me := adminReq(t, client, csrf, http.MethodGet, ts.URL+"/api/admin/me", "")
	defer closeBody(t, me)
	var mr struct {
		User struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(me.Body).Decode(&mr); err != nil {
		t.Fatalf("解析 /me: %v", err)
	}
	if mr.User.Name != "admin" || mr.User.Role != "admin" || mr.CSRFToken != csrf {
		t.Fatalf("/me 内容异常: %+v", mr)
	}

	// 状态变更缺 CSRF 头 → 403(有会话也不行)。
	noCSRF, err := http.NewRequest(http.MethodPost, ts.URL+"/api/admin/channels",
		strings.NewReader(`{"name":"x","driver":"local"}`))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	noCSRF.Header.Set("Content-Type", "application/json")
	ncResp, err := client.Do(noCSRF)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer closeBody(t, ncResp)
	if ncResp.StatusCode != http.StatusForbidden {
		t.Fatalf("缺 CSRF 状态码 = %d, 期望 403", ncResp.StatusCode)
	}

	// 登出后会话失效。
	lo := adminReq(t, client, csrf, http.MethodPost, ts.URL+"/api/admin/auth/logout", "")
	closeBody(t, lo)
	if lo.StatusCode != http.StatusNoContent {
		t.Fatalf("登出状态码 = %d, 期望 204", lo.StatusCode)
	}
	after := adminReq(t, client, csrf, http.MethodGet, ts.URL+"/api/admin/me", "")
	defer closeBody(t, after)
	if after.StatusCode != http.StatusUnauthorized {
		t.Fatalf("登出后 /me 状态码 = %d, 期望 401", after.StatusCode)
	}
}

// TestAdminOnlyChannels 验证渠道管理仅管理员可用:普通用户会话 403。
func TestAdminOnlyChannels(t *testing.T) {
	var authSvc *auth.Service
	ts := newTestServer(t, func(tc *testConfig, env testEnv) {
		authSvc = tc.Router.Auth
		if err := env.Queries.CreateUser(context.Background(), sqlc.CreateUserParams{
			ID: "u-normal", Name: "bob", Role: "user",
		}); err != nil {
			t.Fatalf("建普通用户: %v", err)
		}
	})
	if err := authSvc.SetPassword(t.Context(), "u-normal", "bob-pass"); err != nil {
		t.Fatalf("设置普通用户密码: %v", err)
	}
	client, csrf := loginAs(t, ts, "bob", "bob-pass")
	resp := adminReq(t, client, csrf, http.MethodGet, ts.URL+"/api/admin/channels", "")
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("普通用户访问渠道管理状态码 = %d, 期望 403", resp.StatusCode)
	}
}

// TestRedirectMode 验证 302 省流量模式:允许直链的候选渠道返回重定向。
func TestRedirectMode(t *testing.T) {
	ts := newTestServer(t, func(tc *testConfig, _ testEnv) { tc.Public.RedirectMode = true })

	// 高优先级 + 允许直链的渠道成为主渠道,上传即落在它上面。
	body := fmt.Sprintf(
		`{"name":"cdn","driver":"local","priority":300,"allow_direct_link":true,"config":{"path":%q,"public_base_url":"https://cdn.example.com/pub/"}}`,
		t.TempDir())
	created := postChannel(t, ts, body)
	defer closeBody(t, created)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("创建渠道状态码 = %d, 期望 201", created.StatusCode)
	}

	up := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, up)
	_, imgURL := decodeUpload(t, up)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + imgURL)
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://cdn.example.com/pub/") {
		t.Fatalf("Location = %q, 期望 public_base_url 前缀", loc)
	}
}

// TestServeETag 验证访问响应带强 ETag,且 If-None-Match 命中返回 304。
func TestServeETag(t *testing.T) {
	ts := newTestServer(t, nil)
	resp := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, resp)
	_, imgURL := decodeUpload(t, resp)

	get, err := ts.Client().Get(ts.URL + imgURL)
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer closeBody(t, get)
	etag := get.Header.Get("ETag")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("ETag = %q, 期望带引号的强 ETag", etag)
	}

	// 带 If-None-Match 命中 → 304。
	req, err := http.NewRequest(http.MethodGet, ts.URL+imgURL, nil)
	if err != nil {
		t.Fatalf("构造条件请求: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	cond, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("条件访问: %v", err)
	}
	defer closeBody(t, cond)
	if cond.StatusCode != http.StatusNotModified {
		t.Fatalf("If-None-Match 命中状态码 = %d, 期望 304", cond.StatusCode)
	}
}

// TestMetricsEndpoint 验证 /metrics 暴露 Prometheus 指标,且请求经中间件计入。
func TestMetricsEndpoint(t *testing.T) {
	ts := newTestServer(t, nil)
	// 先产生一次上传请求,让 /api/upload 路由计入。
	up := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	closeBody(t, up)

	resp, err := ts.Client().Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("访问 /metrics: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics 状态码 = %d, 期望 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取 /metrics: %v", err)
	}
	// 上传路由已计入,且用的是路由模板而非原始路径(无高基数)。
	if !bytes.Contains(body, []byte(`imgyoung_http_requests_total{method="POST",route="/api/upload"`)) {
		t.Fatalf("/metrics 未见上传路由计数:\n%s", body)
	}
	// /metrics 自身不计入(避免自我观测噪音)。
	if bytes.Contains(body, []byte(`route="/metrics"`)) {
		t.Error("/metrics 请求不应计入自身指标")
	}
}

// TestChannelCreateValidation 验证创建接口的输入校验:未知字段与矛盾组合拒绝。
func TestChannelCreateValidation(t *testing.T) {
	ts := newTestServer(t, nil)

	unknown := postChannel(t, ts, `{"name":"x","driver":"local","prioritty":5}`)
	defer closeBody(t, unknown)
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("未知字段状态码 = %d, 期望 400", unknown.StatusCode)
	}

	conflict := postChannel(t, ts,
		fmt.Sprintf(`{"name":"y","driver":"local","encrypted":true,"allow_direct_link":true,"config":{"path":%q}}`, t.TempDir()))
	defer closeBody(t, conflict)
	if conflict.StatusCode != http.StatusBadRequest {
		t.Fatalf("加密+直链矛盾组合状态码 = %d, 期望 400", conflict.StatusCode)
	}
}
