package router_test

import (
	"bytes"
	"io"
	"net/http"
	"net/netip"
	"testing"
)

// TestMetricsEndpoint 验证 /metrics 暴露 Prometheus 指标,请求经中间件计入,且只对放行网段开放。
func TestMetricsEndpoint(t *testing.T) {
	ts := newTestServer(t, nil)
	// 先产生一次上传请求,让 /api/upload 路由计入。
	up := uploadReq(t, ts, "a.png", pngBytes(t), nil)
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

	// 客户端地址不在放行网段内(本连接来自回环,放行网段却只有 10/8)一律 403。
	restricted := newTestServer(t, func(tc *testConfig, _ testEnv) {
		tc.Router.MetricsAllow = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	})
	denied, err := restricted.Client().Get(restricted.URL + "/metrics")
	if err != nil {
		t.Fatalf("访问受限 /metrics: %v", err)
	}
	defer closeBody(t, denied)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("网段外访问 /metrics 状态码 = %d, 期望 403", denied.StatusCode)
	}
}
