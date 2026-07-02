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
	"testing"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/server"
	"github.com/x-haose/imgyoung/internal/storage"
)

// testToken 测试用上传 Token。
const testToken = "test-token"

// newTestServer 组装一个使用临时目录(DB + local 渠道)的完整测试服务。
func newTestServer(t *testing.T) *httptest.Server {
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

	s := server.New(server.Config{
		Queries:        db.New(conn),
		Driver:         driver,
		Token:          testToken,
		MaxUploadBytes: 1 << 20,
		Logger:         slog.New(slog.DiscardHandler),
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// pngBytes 生成一张 1x1 的真实 PNG 图片字节。
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
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

// upload 向测试服务发起一次上传请求。
func upload(t *testing.T, ts *httptest.Server, token, filename string, payload []byte) *http.Response {
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

// TestUploadRequiresAuth 验证缺失或错误 Token 一律 401。
func TestUploadRequiresAuth(t *testing.T) {
	ts := newTestServer(t)
	for name, token := range map[string]string{"无Token": "", "错误Token": "wrong"} {
		t.Run(name, func(t *testing.T) {
			resp := upload(t, ts, token, "a.png", pngBytes(t))
			defer closeBody(t, resp)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("状态码 = %d, 期望 401", resp.StatusCode)
			}
		})
	}
}

// TestUploadRejectsNonImage 验证非图片内容(按 magic bytes 判定)返回 415。
func TestUploadRejectsNonImage(t *testing.T) {
	ts := newTestServer(t)
	resp := upload(t, ts, testToken, "fake.png", []byte("plain text disguised as png"))
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("状态码 = %d, 期望 415", resp.StatusCode)
	}
}

// TestUploadAndServeRoundTrip 验证上传后能按短 ID 原样取回,且响应头正确。
func TestUploadAndServeRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	payload := pngBytes(t)

	resp := upload(t, ts, testToken, "a.png", payload)
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("上传状态码 = %d, 期望 200", resp.StatusCode)
	}
	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	if result.ID == "" || result.URL == "" {
		t.Fatalf("上传响应缺字段: %+v", result)
	}

	get, err := ts.Client().Get(ts.URL + result.URL)
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

// TestUploadTooLarge 验证超过单图上限的上传返回 413(内存闸门,见设计定稿)。
func TestUploadTooLarge(t *testing.T) {
	ts := newTestServer(t)
	// 测试服务上限为 1MB,构造 2MB 载荷(以 PNG 头开头以排除 415 干扰)。
	payload := append(pngBytes(t), bytes.Repeat([]byte{0}, 2<<20)...)
	resp := upload(t, ts, testToken, "big.png", payload)
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, 期望 413", resp.StatusCode)
	}
}

// TestServeUnknownID 验证访问不存在的短 ID 返回 404。
func TestServeUnknownID(t *testing.T) {
	ts := newTestServer(t)
	resp, err := ts.Client().Get(ts.URL + "/i/nonexistent0")
	if err != nil {
		t.Fatalf("访问图片: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", resp.StatusCode)
	}
}
