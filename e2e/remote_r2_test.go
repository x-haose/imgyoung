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
	"net/http"
	"os"
	"testing"
	"time"
)

// TestRemoteR2RoundTrip 真实 R2 渠道:上传写入 R2 → 取回字节一致 → TTL 到期后对象删除。
func TestRemoteR2RoundTrip(t *testing.T) {
	t.Parallel()
	endpoint := os.Getenv("IMGYOUNG_E2E_R2_ENDPOINT")
	key := os.Getenv("IMGYOUNG_E2E_R2_KEY")
	secret := os.Getenv("IMGYOUNG_E2E_R2_SECRET")
	path := os.Getenv("IMGYOUNG_E2E_R2_PATH")
	if endpoint == "" || key == "" || secret == "" || path == "" {
		t.Skip("未提供 R2 凭证环境变量,跳过真实远端渠道验证")
	}

	inst := startInstance(t, map[string]any{
		"cleanup_sweep_seconds": 2,
		"delete_grace_minutes":  0,
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
	waitUntil(t, 20*time.Second, "R2 图 TTL 到期后引用回收", func() bool {
		resp := fetch(t, inst, url, "")
		code := resp.StatusCode
		_ = resp.Body.Close()
		return code == http.StatusNotFound
	})
}
