package server_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/imaging"
	"github.com/x-haose/imgyoung/internal/server"
	"github.com/x-haose/imgyoung/internal/storage"
	"github.com/x-haose/imgyoung/internal/upload"
)

// testToken 测试用上传 Token。
const testToken = "test-token"

// newTestServer 组装一个使用临时目录(DB + local 渠道)的完整测试服务。
// @param mutate 可选的配置修改器,用于按用例定制防盗链/BaseURL 等
func newTestServer(t *testing.T, mutate func(*server.Config)) *httptest.Server {
	t.Helper()
	dir := t.TempDir()

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

	driver, err := storage.NewRcloneDriver(t.Context(), filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatalf("创建测试存储驱动: %v", err)
	}
	queries := db.New(conn)
	svc := upload.New(upload.Config{
		Conn:      conn,
		Queries:   queries,
		Driver:    driver,
		MaxPixels: 1 << 20,
	})

	cfg := server.Config{
		Queries:           queries,
		Driver:            driver,
		Upload:            svc,
		Token:             testToken,
		MaxUploadBytes:    1 << 20,
		AllowEmptyReferer: true,
		DefaultEXIFPolicy: imaging.PolicyPassthrough,
		Logger:            slog.New(slog.DiscardHandler),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	ts := httptest.NewServer(server.New(cfg).Handler())
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

// upload 向测试服务发起一次上传请求;extraHeaders 追加请求头。
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
	ts := newTestServer(t, func(c *server.Config) { c.BaseURL = "https://img.example.com" })
	resp := uploadReq(t, ts, testToken, "a.png", pngBytes(t), nil)
	defer closeBody(t, resp)
	_, imgURL := decodeUpload(t, resp)
	if !strings.HasPrefix(imgURL, "https://img.example.com/i/") {
		t.Fatalf("url = %q, 期望以 BaseURL 开头的绝对地址", imgURL)
	}
}

// TestRefererProtection 验证防盗链:白名单命中放行、未命中 403、空 Referer 按配置。
func TestRefererProtection(t *testing.T) {
	ts := newTestServer(t, func(c *server.Config) {
		c.RefererWhitelist = []string{"*.ok.com"}
		c.AllowEmptyReferer = true
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

// TestUploadTooLarge 验证超过单图上限的上传返回 413(内存闸门,见设计定稿)。
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
