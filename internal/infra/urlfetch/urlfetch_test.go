package urlfetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// newFetcher 造一个上限固定的抓取器(保留真实地址判定)。
func newFetcher(maxBytes int64) *Fetcher {
	return New(func() int64 { return maxBytes }, 5*time.Second)
}

// newLoopbackFetcher 造一个放行环回地址的抓取器。
// @Description: httptest 一律监听环回,真实判定会在建连阶段拦下、成功路径无从覆盖;
// 故此处只替换地址准入,协议/跳数/大小三道闸仍是产品实现。
func newLoopbackFetcher(t *testing.T, maxBytes int64) *Fetcher {
	t.Helper()
	f := newFetcher(maxBytes)
	f.allowAddr = func(netip.Addr) bool { return true }
	return f
}

// TestFetchSuccess 正常抓取:返回完整字节与远端 Content-Type。
func TestFetchSuccess(t *testing.T) {
	const payload = "fake-image-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	data, ctype, err := newLoopbackFetcher(t, 1<<20).Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if string(data) != payload {
		t.Errorf("内容 = %q, 期望 %q", data, payload)
	}
	if ctype != "image/png" {
		t.Errorf("Content-Type = %q, 期望 image/png", ctype)
	}
}

// TestFetchRejectsNonHTTPScheme 非 http/https 协议一律拒绝,不给 file:// 之类读本地文件的机会。
func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	f := newFetcher(1 << 20)
	for _, raw := range []string{
		"file:///etc/passwd", "gopher://x/1", "ftp://example.com/a.png", "data:image/png;base64,AA",
	} {
		if _, _, err := f.Fetch(context.Background(), raw); !errors.Is(err, ErrScheme) {
			t.Errorf("%s 期望 ErrScheme,得 %v", raw, err)
		}
	}
}

// TestFetchRejectsPrivateTarget 指向环回地址的 URL 在建连阶段被拦下,拿不到内网响应体。
func TestFetchRejectsPrivateTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	if _, _, err := newFetcher(1<<20).Fetch(context.Background(), srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("环回地址期望 ErrBlockedAddress,得 %v", err)
	}
}

// TestFetchRedirectHopLimit 重定向跳数超上限即失败,不被无限跳转拖住。
func TestFetchRedirectHopLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := newLoopbackFetcher(t, 1<<20).Fetch(context.Background(), srv.URL)
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("无限重定向期望 ErrTooManyRedirects,得 %v", err)
	}
}

// TestFetchSizeLimit 超上限即报错,不返回被截断的半张图;谎报 Content-Length 也拦得住。
func TestFetchSizeLimit(t *testing.T) {
	big := strings.Repeat("x", 1024)
	honest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer honest.Close()
	// 不声明长度(chunked),迫使大小闸在读取阶段生效而非靠 Content-Length 预检。
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Transfer-Encoding", "chunked")
		for range 16 {
			_, _ = w.Write([]byte(strings.Repeat("y", 64)))
			w.(http.Flusher).Flush()
		}
	}))
	defer chunked.Close()

	f := newLoopbackFetcher(t, 100)
	for name, url := range map[string]string{"声明长度": honest.URL, "分块传输": chunked.URL} {
		if _, _, err := f.Fetch(context.Background(), url); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s 期望 ErrTooLarge,得 %v", name, err)
		}
	}
}

// TestFetchNonSuccessStatus 远端非 2xx 即报错,不把错误页当图片存下来。
func TestFetchNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	if _, _, err := newLoopbackFetcher(t, 1<<20).Fetch(context.Background(), srv.URL); !errors.Is(err, ErrStatus) {
		t.Fatalf("404 期望 ErrStatus,得 %v", err)
	}
}

// TestIsPublicBlocksInfrastructureAddresses 地址判定的正反面:基础设施地址一律不可抓,公网地址放行。
func TestIsPublicBlocksInfrastructureAddresses(t *testing.T) {
	blocked := []string{
		"127.0.0.1",        // 环回
		"::1",              // IPv6 环回
		"10.0.0.5",         // 私有段
		"172.16.3.9",       // 私有段
		"192.168.1.1",      // 私有段
		"169.254.169.254",  // 云元数据
		"::ffff:127.0.0.1", // IPv4-mapped 环回(不解包就会漏过)
		"::ffff:10.0.0.1",  // IPv4-mapped 私有段
		"fd00::1",          // IPv6 唯一本地
		"fe80::1",          // IPv6 link-local
		"::",               // 未指定
		"0.0.0.0",          // 未指定
		"100.64.0.1",       // 运营商级 NAT
		"192.0.0.1",        // IETF 协议分配
		"0.1.2.3",          // 0.0.0.0/8 整段,不只单个 0.0.0.0
		"198.18.0.1",       // 基准测试网段
		"::127.0.0.1",      // IPv4-compatible 外壳
		"64:ff9b::7f00:1",  // NAT64 外壳裹环回
		"64:ff9b::a00:1",   // NAT64 外壳裹私有段
		"2002:7f00:1::",    // 6to4 外壳裹环回
		"ff02::1",          // IPv6 多播
		"224.0.0.1",        // 多播
		"255.255.255.255",  // 广播
	}
	for _, raw := range blocked {
		ip, err := netip.ParseAddr(raw)
		if err != nil {
			t.Fatalf("测试地址 %s 解析失败: %v", raw, err)
		}
		if isPublic(ip) {
			t.Errorf("%s 应被判为不可抓取", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "203.0.113.7", "2001:4860:4860::8888"} {
		ip, err := netip.ParseAddr(raw)
		if err != nil {
			t.Fatalf("测试地址 %s 解析失败: %v", raw, err)
		}
		if !isPublic(ip) {
			t.Errorf("%s 应可抓取", raw)
		}
	}
}

// TestCheckDialAddressRejectsMalformed 地址格式异常按拒绝处理,不放行未知形态。
func TestCheckDialAddressRejectsMalformed(t *testing.T) {
	f := newFetcher(1 << 20)
	for _, addr := range []string{"", "example.com:80", "1.2.3.4"} {
		if err := f.checkDialAddress(addr); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%q 期望 ErrBlockedAddress,得 %v", addr, err)
		}
	}
	if err := f.checkDialAddress(net.JoinHostPort("8.8.8.8", "443")); err != nil {
		t.Errorf("公网地址不应被拒: %v", err)
	}
}
