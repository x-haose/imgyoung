//go:build e2e

// 真实远端渠道(S3/Cloudflare R2)端到端验证。
// 凭证从环境变量读,不硬编码;缺任一即 skip。用独立前缀,收尾靠 TTL 让服务自清。
// 运行示例:
//
//	IMGYOUNG_E2E_R2_ENDPOINT=... IMGYOUNG_E2E_R2_KEY=... \
//	IMGYOUNG_E2E_R2_SECRET=... IMGYOUNG_E2E_R2_PATH=bucket/prefix \
//	go test -tags e2e -run TestRemoteR2 ./e2e/
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// channelInfo 对应 /api/admin/channels 返回项。
type channelInfo struct {
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Healthy bool   `json:"healthy"`
}

// listChannels 以管理员会话拉取渠道列表。
func listChannels(t *testing.T, inst *instance) []channelInfo {
	t.Helper()
	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/channels", "")
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out []channelInfo
	_ = json.Unmarshal(raw, &out)
	return out
}

// channelHealthy 判断指定渠道当前是否健康。
func channelHealthy(t *testing.T, inst *instance, name string) bool {
	t.Helper()
	for _, c := range listChannels(t, inst) {
		if c.Name == name {
			return c.Healthy
		}
	}
	return false
}

// TestRemoteR2RoundTrip 真实 R2 渠道:上传写入 R2 → 取回字节一致 → TTL 到期后对象删除。
func TestRemoteR2RoundTrip(t *testing.T) {
	endpoint := os.Getenv("IMGYOUNG_E2E_R2_ENDPOINT")
	key := os.Getenv("IMGYOUNG_E2E_R2_KEY")
	secret := os.Getenv("IMGYOUNG_E2E_R2_SECRET")
	path := os.Getenv("IMGYOUNG_E2E_R2_PATH")
	if endpoint == "" || key == "" || secret == "" || path == "" {
		t.Skip("未提供 R2 凭证环境变量,跳过真实远端渠道验证")
	}

	inst := startInstance(t, map[string]string{
		"IMGYOUNG_CLEANUP_SWEEP_SECONDS": "2",
		"IMGYOUNG_DELETE_GRACE_MINUTES":  "0",
	})

	// 建 s3 渠道,优先级高于内置 local(100),使其成为主渠道、上传同步写入 R2。
	cfg := map[string]string{
		"provider":          "Cloudflare",
		"access_key_id":     key,
		"secret_access_key": secret,
		"region":            "auto",
		"endpoint":          endpoint,
		"path":              path, // bucket/prefix,对象都落此前缀下
	}
	jb, _ := json.Marshal(map[string]any{
		"name": "r2", "driver": "s3", "priority": 200, "config": cfg,
	})
	createChannel(t, inst, string(jb))

	// 等健康探测把 R2 标健康(Stat 探测走真实网络)。
	waitUntil(t, 30*time.Second, "R2 渠道就绪", func() bool {
		return channelHealthy(t, inst, "r2")
	})

	// 上传真实图片(ttl=3s 便于收尾自清)。
	src := procJPEG(400, 300, 0)
	_, url := mustUpload(t, inst, src, map[string]string{"ttl": "3"}, nil)

	// 取回:字节应与上传完全一致(经 R2 Put→Open 往返)。
	got := fetchBytes(t, inst, url)
	if !bytes.Equal(got, src) {
		t.Fatalf("R2 往返字节不一致:上传 %d 字节,取回 %d 字节", len(src), len(got))
	}

	// TTL 到期 + 清理:取回应变 404(引用回收 + R2 对象删除任务已入队执行)。
	deleted := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp := fetch(t, inst, url, "")
		code := resp.StatusCode
		_ = resp.Body.Close()
		if code == 404 {
			deleted = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !deleted {
		t.Error("R2 图 TTL 到期后仍可取回(引用未回收)")
	}
}
