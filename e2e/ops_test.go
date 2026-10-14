//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// webhookSink 收集告警 Webhook 投递的本地 HTTP 服务。
type webhookSink struct {
	mu  sync.Mutex
	got []map[string]string // 收到的请求体,按到达顺序
}

// ServeHTTP 记录请求体并回 204。
func (s *webhookSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.got = append(s.got, body)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// has 报告是否收到过指定类型、对象与状态的投递。
func (s *webhookSink) has(kind, target, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.got {
		if b["kind"] == kind && b["target"] == target && b["status"] == status {
			return true
		}
	}
	return false
}

// openAlertTargets 读未恢复告警,返回 kind/target 集合。
func openAlertTargets(t *testing.T, inst *instance, client *http.Client, csrf string) map[string]bool {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/alerts", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("告警列表状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		Items []struct {
			Kind   string `json:"kind"`
			Target string `json:"target"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析告警列表: %v", err)
	}
	out := map[string]bool{}
	for _, it := range lr.Items {
		out[it.Kind+"/"+it.Target] = true
	}
	return out
}

// TestAlertWebhookOnChannelUnhealthy 渠道后端不可达时告警开启并 POST 到 Webhook、出现在告警列表;
// 后端恢复后告警恢复并再投一次,列表里不再有它。
func TestAlertWebhookOnChannelUnhealthy(t *testing.T) {
	t.Parallel()
	sink := &webhookSink{}
	srv := httptest.NewServer(sink)
	t.Cleanup(srv.Close)
	inst := startInstance(t, map[string]any{"alert_webhook_url": srv.URL})
	admin, csrf := adminSession(t, inst)

	dir := filepath.Join(inst.dataDir, "flip")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	createChannel(t, inst, fmt.Sprintf(`{"name":"flip","driver":"local","priority":50,"config":{"path":%q}}`, dir))
	// 把目录换成同名普通文件,探测 Stat 报非 NotFound 错 → 不健康。
	_ = os.RemoveAll(dir)
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, "不健康告警投递到 Webhook", func() bool {
		return sink.has("channel_unhealthy", "flip", "open")
	})
	if !openAlertTargets(t, inst, admin, csrf)["channel_unhealthy/flip"] {
		t.Fatal("告警列表里没有 flip 的不健康告警")
	}

	_ = os.Remove(dir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, "恢复投递到 Webhook", func() bool {
		return sink.has("channel_unhealthy", "flip", "resolved")
	})
	if openAlertTargets(t, inst, admin, csrf)["channel_unhealthy/flip"] {
		t.Fatal("恢复后告警列表里仍有 flip 的不健康告警")
	}
}

// backupNames 读备份快照列表的名字集合。
func backupNames(t *testing.T, inst *instance, client *http.Client, csrf string) map[string]bool {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/backups", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("快照列表状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		Items []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析快照列表: %v", err)
	}
	out := map[string]bool{}
	for _, it := range lr.Items {
		if it.Size > 0 {
			out[it.Name] = true
		}
	}
	return out
}

// TestBackupListDownloadRestore 备份闭环:立即备份得到快照并出现在列表,下载得到 SQLite 文件;
// 快照之后再传一张图,用该快照恢复 → 进程自行退出,重启后换入:快照前的图仍在,快照后的引用不存在。
func TestBackupListDownloadRestore(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	payload := pngBytes(t)
	_, before := mustUpload(t, inst, payload, nil, nil)

	created := adminReq(t, admin, csrf, http.MethodPost, inst.baseURL+"/api/admin/backups", "")
	var snap struct {
		Name string `json:"name"`
	}
	if created.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(created.Body)
		t.Fatalf("立即备份状态码 = %d, 期望 201; body=%s", created.StatusCode, raw)
	}
	if err := json.NewDecoder(created.Body).Decode(&snap); err != nil {
		t.Fatalf("解析备份响应: %v", err)
	}
	_ = created.Body.Close()
	if !strings.HasPrefix(snap.Name, "imgyoung-") || !backupNames(t, inst, admin, csrf)[snap.Name] {
		t.Fatalf("新快照 %q 未出现在列表", snap.Name)
	}

	download := adminReq(t, admin, csrf, http.MethodGet, inst.baseURL+"/api/admin/backups/"+snap.Name, "")
	raw, err := io.ReadAll(download.Body)
	_ = download.Body.Close()
	if err != nil || download.StatusCode != http.StatusOK || !bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) {
		t.Fatalf("下载快照: 状态码 %d err=%v 前缀=%q", download.StatusCode, err, raw[:min(16, len(raw))])
	}

	_, after := mustUpload(t, inst, pngBytes(t), nil, nil)
	restore := adminReq(t, admin, csrf, http.MethodPost, inst.baseURL+"/api/admin/backups/"+snap.Name+"/restore", "")
	code := restore.StatusCode
	_ = restore.Body.Close()
	if code != http.StatusAccepted {
		t.Fatalf("恢复状态码 = %d, 期望 202", code)
	}
	exited := make(chan error, 1)
	go func() { exited <- inst.cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("恢复后进程应以零码退出: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("恢复后进程未退出")
	}
	if _, err := os.Stat(filepath.Join(inst.dataDir, "imgyoung.db.restore")); err != nil {
		t.Fatalf("停机后应留有暂存快照: %v", err)
	}

	inst2 := startInstanceAt(t, inst, strings.TrimPrefix(inst.baseURL, "http://"))
	kept, err := filepath.Glob(filepath.Join(inst.dataDir, "imgyoung.db.pre-restore-*"))
	if err != nil || len(kept) == 0 {
		t.Fatalf("换入后原库应留存: %v %v", kept, err)
	}
	if got := fetchBytes(t, inst2, before); !bytes.Equal(got, payload) {
		t.Fatal("快照前上传的图恢复后不可访问或字节不一致")
	}
	gone := fetch(t, inst2, after, "")
	goneCode := gone.StatusCode
	_ = gone.Body.Close()
	if goneCode != http.StatusNotFound {
		t.Fatalf("快照后上传的引用恢复后状态码 = %d, 期望 404", goneCode)
	}
}
