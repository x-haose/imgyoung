package cdn

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeCF 收集 purge 调用的假 Cloudflare API。
type fakeCF struct {
	mu      sync.Mutex
	batches [][]string // 每次调用收到的 files 列表
	status  int        // 响应状态码
	body    string     // 响应体
}

// handler 校验路径与鉴权头,记录批次并按配置回包。
func (f *fakeCF) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/zones/zone123/purge_cache") {
			t.Errorf("purge 路径错误: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok456" {
			t.Errorf("鉴权头错误: %q", got)
		}
		var req struct {
			Files []string `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("请求体非 JSON: %v", err)
		}
		f.mu.Lock()
		f.batches = append(f.batches, req.Files)
		f.mu.Unlock()
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	}
}

// newPurger 构造指向假 API 的执行器。
func newPurger(srvURL string) *CloudflarePurger {
	return NewCloudflareAt(srvURL, "zone123", "tok456")
}

// TestPurgeBatching 验证 URL 收集与分批:两批满额加 5 个余数按三批顺序发出,内容与顺序完整。
func TestPurgeBatching(t *testing.T) {
	fake := &fakeCF{status: http.StatusOK, body: `{"success":true}`}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	urls := make([]string, 2*purgeBatchSize+5)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://img.example.com/i/id%03d", i)
	}
	if err := newPurger(srv.URL).Purge(t.Context(), urls); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if len(fake.batches) != 3 {
		t.Fatalf("批次数 = %d, 期望 3", len(fake.batches))
	}
	wantSizes := []int{purgeBatchSize, purgeBatchSize, 5}
	got := 0
	for i, batch := range fake.batches {
		if len(batch) != wantSizes[i] {
			t.Errorf("第 %d 批大小 = %d, 期望 %d", i, len(batch), wantSizes[i])
		}
		for _, u := range batch {
			if u != urls[got] {
				t.Fatalf("URL 顺序错乱: 第 %d 个 = %q, 期望 %q", got, u, urls[got])
			}
			got++
		}
	}
}

// TestPurgeEmptyNoCall 验证空列表不发起任何调用。
func TestPurgeEmptyNoCall(t *testing.T) {
	fake := &fakeCF{status: http.StatusOK, body: `{"success":true}`}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	if err := newPurger(srv.URL).Purge(t.Context(), nil); err != nil {
		t.Fatalf("Purge(nil): %v", err)
	}
	if len(fake.batches) != 0 {
		t.Errorf("空列表仍发起了 %d 次调用", len(fake.batches))
	}
}

// TestPurgeAPIFailure 验证失败路径:信封显式 success=false 即报错(有明细带出错误码,
// 无明细给通用文案,2xx 也不豁免),非 2xx 亦报错,且错误消息不含 Token。
func TestPurgeAPIFailure(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			"信封失败", http.StatusForbidden,
			`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`, "10000",
		},
		{"信封失败无明细且 2xx", http.StatusOK, `{"success":false,"errors":[]}`, "success=false"},
		{"非 2xx 无信封", http.StatusBadGateway, "gateway error", "502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeCF{status: tc.status, body: tc.body}
			srv := httptest.NewServer(fake.handler(t))
			defer srv.Close()

			err := newPurger(srv.URL).Purge(t.Context(), []string{"https://img.example.com/i/x"})
			if err == nil {
				t.Fatal("失败响应未报错")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误消息 %q 未包含 %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "tok456") {
				t.Errorf("错误消息泄露 Token: %q", err)
			}
		})
	}
}

// TestPurgeNonEnvelope2xxOK 验证 success 字段缺失的非信封 2xx 响应仍视为成功。
func TestPurgeNonEnvelope2xxOK(t *testing.T) {
	for _, body := range []string{`{}`, "plain ok"} {
		fake := &fakeCF{status: http.StatusOK, body: body}
		srv := httptest.NewServer(fake.handler(t))
		err := newPurger(srv.URL).Purge(t.Context(), []string{"https://img.example.com/i/x"})
		srv.Close()
		if err != nil {
			t.Errorf("非信封 2xx(body %q)误判失败: %v", body, err)
		}
	}
}
