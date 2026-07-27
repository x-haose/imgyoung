package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// stubFetcher 抓取器替身:真实抓取器按设计拒绝一切内网地址,而测试只能监听环回,
// 故成功路径必须用替身覆盖;SSRF 判定本身由 urlfetch 包的单测保证。
type stubFetcher struct {
	data  []byte // 返回的字节
	ctype string // 返回的 Content-Type
	err   error  // 非 nil 时返回此错误
	got   string // 记录收到的地址,供断言透传正确
}

// Fetch 返回预置结果并记录请求地址。
func (s *stubFetcher) Fetch(_ context.Context, rawURL string) ([]byte, string, error) {
	s.got = rawURL
	if s.err != nil {
		return nil, "", s.err
	}
	return s.data, s.ctype, nil
}

// pngPayload 生成一张可解码的小 PNG。
func pngPayload(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// postUploadURL 以有效 Token 发一次 URL 转存请求。
func postUploadURL(t *testing.T, ts *httptest.Server, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload/url", strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发送请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应: %v", err)
	}
	return resp, raw
}

// TestUploadURLSuccess 转存成功路径:抓回的字节走完整上传管道,响应形态与直传一致,
// 且返回的地址可直接取回同一份字节。
func TestUploadURLSuccess(t *testing.T) {
	payload := pngPayload(t)
	stub := &stubFetcher{data: payload, ctype: "image/png"}
	ts := newTestServer(t, func(cfg *testConfig, _ testEnv) {
		cfg.Public.Fetch = stub
	})

	resp, raw := postUploadURL(t, ts, `{"url":"https://example.com/dir/photo.png"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
	}
	if stub.got != "https://example.com/dir/photo.png" {
		t.Errorf("抓取地址 = %q, 期望原样透传", stub.got)
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if out.ID == "" || !strings.HasSuffix(out.URL, "/i/"+out.ID) {
		t.Fatalf("响应形态异常: %s", raw)
	}
	// 转存进来的图必须能按短链取回,且字节与远端一致。
	fetched, err := http.Get(ts.URL + "/i/" + out.ID)
	if err != nil {
		t.Fatalf("访问短链: %v", err)
	}
	defer func() { _ = fetched.Body.Close() }()
	got, err := io.ReadAll(fetched.Body)
	if err != nil {
		t.Fatalf("读取图片: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("按短链取回的字节与转存来源不一致")
	}
}

// TestUploadURLAppliesTTLAndName 转存同样吃 ttl 与 file_name;ttl 数字与字符串两种写法都收。
func TestUploadURLAppliesTTLAndName(t *testing.T) {
	stub := &stubFetcher{data: pngPayload(t), ctype: "image/png"}
	ts := newTestServer(t, func(cfg *testConfig, _ testEnv) {
		cfg.Public.Fetch = stub
	})

	for name, body := range map[string]string{
		"ttl 为数字":  `{"url":"https://example.com/a.png","ttl":3600}`,
		"ttl 为字符串": `{"url":"https://example.com/a.png","ttl":"3600"}`,
		"ttl 为永久":  `{"url":"https://example.com/a.png","ttl":"never"}`,
		"指定文件名":    `{"url":"https://example.com/a.png","file_name":"custom.png"}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp, raw := postUploadURL(t, ts, body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
			}
		})
	}
}

// TestUploadURLRejectsBadRequest 请求体自身的问题一律 400,不进抓取。
func TestUploadURLRejectsBadRequest(t *testing.T) {
	stub := &stubFetcher{data: pngPayload(t), ctype: "image/png"}
	ts := newTestServer(t, func(cfg *testConfig, _ testEnv) {
		cfg.Public.Fetch = stub
	})

	for name, body := range map[string]string{
		"非 JSON": `not-json`,
		"缺 url":  `{}`,
		"url 为空": `{"url":"   "}`,
		"未知字段":   `{"url":"https://example.com/a.png","evil":1}`,
		"ttl 非法": `{"url":"https://example.com/a.png","ttl":"soon"}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp, raw := postUploadURL(t, ts, body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, 期望 400; body=%s", resp.StatusCode, raw)
			}
		})
	}
}

// TestUploadURLMapsFetchErrors 抓取失败按原因映射状态码,且不回显远端细节。
func TestUploadURLMapsFetchErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"协议不合法": {urlfetch.ErrScheme, http.StatusBadRequest},
		"地址被拒":  {urlfetch.ErrBlockedAddress, http.StatusBadRequest},
		"跳数超限":  {urlfetch.ErrTooManyRedirects, http.StatusBadGateway},
		"远端过大":  {urlfetch.ErrTooLarge, http.StatusRequestEntityTooLarge},
		"远端报错":  {urlfetch.ErrStatus, http.StatusBadGateway},
		"网络故障":  {errors.New("dial tcp: connection refused"), http.StatusBadGateway},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ts := newTestServer(t, func(cfg *testConfig, _ testEnv) {
				cfg.Public.Fetch = &stubFetcher{err: tc.err}
			})
			resp, raw := postUploadURL(t, ts, `{"url":"https://example.com/a.png"}`)
			if resp.StatusCode != tc.want {
				t.Fatalf("状态码 = %d, 期望 %d; body=%s", resp.StatusCode, tc.want, raw)
			}
			if strings.Contains(string(raw), "connection refused") {
				t.Errorf("响应泄露了底层网络细节: %s", raw)
			}
		})
	}
}

// TestUploadURLRejectsNonImage 抓回的内容不是图片时,与直传同样被净化管道拦下。
func TestUploadURLRejectsNonImage(t *testing.T) {
	ts := newTestServer(t, func(cfg *testConfig, _ testEnv) {
		cfg.Public.Fetch = &stubFetcher{data: []byte("<html>not an image</html>"), ctype: "image/png"}
	})
	resp, raw := postUploadURL(t, ts, `{"url":"https://example.com/a.png"}`)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("状态码 = %d, 期望 415; body=%s", resp.StatusCode, raw)
	}
}
