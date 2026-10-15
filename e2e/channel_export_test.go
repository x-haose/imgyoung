//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rclone/rclone/fs"

	"github.com/x-haose/imgyoung/internal/core/testenv"

	// 导出的配置文件只用到这两种后端:WebDAV 渠道与其上的 crypt 层。
	_ "github.com/rclone/rclone/backend/crypt"
	_ "github.com/rclone/rclone/backend/webdav"
)

// exportedChannel 元数据导出里的渠道项中本用例关心的字段。
type exportedChannel struct {
	ID        int64  `json:"id"`         // 渠道 id
	CreatedAt string `json:"created_at"` // 创建时刻
}

// storageKeyOf 经元数据导出查出图片的对象键,并断言渠道项带 id 与创建时刻。
// @param imageID 图片短 ID
// @param channelID 须出现在导出里的渠道
// @return 该图在各渠道内的对象键
func storageKeyOf(t *testing.T, inst *instance, client *http.Client, csrf, imageID string, channelID int64) string {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/export", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("元数据导出状态码 = %d, 期望 200", resp.StatusCode)
	}
	var payload struct {
		Channels []exportedChannel `json:"channels"`
		Images   []struct {
			ID     string `json:"id"`
			BlobID int64  `json:"blob_id"`
		} `json:"images"`
		Blobs []struct {
			ID         int64  `json:"id"`
			StorageKey string `json:"storage_key"`
		} `json:"blobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("解析元数据导出: %v", err)
	}
	if !hasChannel(payload.Channels, channelID) {
		t.Fatalf("元数据导出的渠道里没有带创建时刻的渠道 %d: %+v", channelID, payload.Channels)
	}
	for _, img := range payload.Images {
		for _, b := range payload.Blobs {
			if img.ID == imageID && b.ID == img.BlobID {
				return b.StorageKey
			}
		}
	}
	t.Fatalf("元数据导出里找不到图片 %s 的对象键", imageID)
	return ""
}

// hasChannel 判断导出的渠道里有没有指定 id 且带创建时刻的一项。
// @param channels 导出的渠道
// @param channelID 渠道 id
// @return 有为 true
func hasChannel(channels []exportedChannel, channelID int64) bool {
	for _, c := range channels {
		if c.ID == channelID && c.CreatedAt != "" {
			return true
		}
	}
	return false
}

// downloadRcloneConfig 下载渠道的 rclone 配置并断言附件、不缓存等响应头。
// @param channelID 渠道 id
// @return 配置文件全文
func downloadRcloneConfig(t *testing.T, inst *instance, client *http.Client, csrf string, channelID int64) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/admin/channels/%d/rclone-config", inst.baseURL, channelID)
	resp := adminReq(t, client, csrf, http.MethodGet, url, "")
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("下载 rclone 配置: 状态码 %d err=%v body=%s", resp.StatusCode, err, raw)
	}
	wantFile := fmt.Sprintf(`filename="imgyoung-channel-%d.rclone.conf"`, channelID)
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, wantFile) {
		t.Errorf("Content-Disposition = %q, 期望附件 %s", cd, wantFile)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, 期望 no-store", cc)
	}
	return string(raw)
}

// loadRcloneConfig 让本进程的 rclone 从给定文本加载配置,测试结束恢复原有的配置路径与存储。
// @param text 配置文件全文
func loadRcloneConfig(t *testing.T, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rclone.conf")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("写配置文件: %v", err)
	}
	testenv.UseRcloneConfig(t, path)
}

// readWithRclone 按 rclone 路径构造 Fs 并读出一个对象。
// @param root rclone 路径,形如 remote:dir
// @param key 对象键
// @return 对象内容
func readWithRclone(t *testing.T, root, key string) []byte {
	t.Helper()
	f, err := fs.NewFs(t.Context(), root)
	if err != nil {
		t.Fatalf("构造 %s: %v", root, err)
	}
	obj, err := f.NewObject(t.Context(), key)
	if err != nil {
		t.Fatalf("在 %s 查找对象 %s: %v", root, key, err)
	}
	rc, err := obj.Open(t.Context())
	if err != nil {
		t.Fatalf("打开对象: %v", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("读取对象: %v", err)
	}
	return data
}

// TestChannelRcloneConfigSelfRescue 脱离服务自救:加密的 WebDAV 渠道上的图,只凭下载的 rclone 配置与元数据导出,
// 用标准 rclone 读回的字节与服务输出的一致;未知渠道 404。
// @Description: WebDAV 密码与 crypt 密码都须按 rclone 的规则以 obscure 形式写进配置,写错任何一个都读不回
// (前者认证失败,后者解不开对象名)。rclone 的配置是进程级全局状态,本用例不与其他用例并行。
func TestChannelRcloneConfigSelfRescue(t *testing.T) {
	davURL := startWebDAVServer(t)
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	body, err := json.Marshal(map[string]any{
		"name": "dav-vault", "template": "webdav", "priority": 500, "encrypted": true,
		"inputs": map[string]string{
			"url": davURL + "/", "vendor": "other", "user": davUser, "pass": davPass, "path": "vault",
		},
	})
	if err != nil {
		t.Fatalf("编码请求体: %v", err)
	}
	channelID := createChannel(t, inst, string(body))
	imageID, imageURL := mustUpload(t, inst, pngBytes(t), nil, nil)
	served := fetchBytes(t, inst, imageURL)

	key := storageKeyOf(t, inst, client, csrf, imageID, channelID)
	text := downloadRcloneConfig(t, inst, client, csrf, channelID)
	loadRcloneConfig(t, text)
	got := readWithRclone(t, fmt.Sprintf("imgyoung-%d-crypt:", channelID), key)
	if !bytes.Equal(got, served) {
		t.Fatalf("rclone 读回 %d 字节,与服务输出的 %d 字节不一致", len(got), len(served))
	}

	missing := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/channels/99999/rclone-config", "")
	code := missing.StatusCode
	_ = missing.Body.Close()
	if code != http.StatusNotFound {
		t.Fatalf("未知渠道导出状态码 = %d, 期望 404", code)
	}
}
