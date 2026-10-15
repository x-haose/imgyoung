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
	"slices"
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

// snapshotItem 快照列表项与立即备份响应里本测试关心的字段。
type snapshotItem struct {
	Name     string   `json:"name"`
	Size     int64    `json:"size"`
	Channels []string `json:"channels"`
}

// backupChannels 读备份快照列表:快照名 → 持有它的渠道名(按优先级降序)。
func backupChannels(t *testing.T, inst *instance, client *http.Client, csrf string) map[string][]string {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/backups", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("快照列表状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		Items []snapshotItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析快照列表: %v", err)
	}
	out := map[string][]string{}
	for _, it := range lr.Items {
		if it.Size > 0 {
			out[it.Name] = it.Channels
		}
	}
	return out
}

// createBackup 立即备份一次,断言 201,返回新快照。
func createBackup(t *testing.T, inst *instance, client *http.Client, csrf string) snapshotItem {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/backups", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("立即备份状态码 = %d, 期望 201; body=%s", resp.StatusCode, raw)
	}
	var snap snapshotItem
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("解析备份响应: %v", err)
	}
	return snap
}

// TestBackupListDownloadRestore 备份闭环:立即备份把同一份快照写到每个健康渠道并出现在列表;
// 优先级最高的渠道上的快照丢了,下载与恢复改从下一个持有它的渠道读。快照之后再传一张图,
// 用该快照恢复 → 进程自行退出,重启后换入:快照前的图仍在,快照后的引用不存在。
func TestBackupListDownloadRestore(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"mirror","driver":"local","priority":50,"config":{"path":%q}}`, t.TempDir()))
	payload := pngBytes(t)
	_, before := mustUpload(t, inst, payload, nil, nil)

	snap := createBackup(t, inst, admin, csrf)
	want := []string{"local", "mirror"}
	if !strings.HasPrefix(snap.Name, "imgyoung-") || !slices.Equal(snap.Channels, want) {
		t.Fatalf("新快照 = %+v, 期望写到 %v", snap, want)
	}
	if got := backupChannels(t, inst, admin, csrf)[snap.Name]; !slices.Equal(got, want) {
		t.Fatalf("列表里新快照所在渠道 = %v, 期望 %v", got, want)
	}

	// 内置 local 渠道即 dataDir/blobs:删掉它上面的这份快照,只剩 mirror 持有。
	if err := os.Remove(filepath.Join(inst.dataDir, "blobs", "_imgyoung", "backup", snap.Name)); err != nil {
		t.Fatalf("删除 local 上的快照: %v", err)
	}
	if got := backupChannels(t, inst, admin, csrf)[snap.Name]; !slices.Equal(got, []string{"mirror"}) {
		t.Fatalf("local 上的快照删除后所在渠道 = %v, 期望只剩 mirror", got)
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

// TestBackupReportsFailedChannel 有渠道写不进快照时立即备份如实失败:错误点名该渠道,其余渠道照常写成,
// 列表里这份快照只记在写成的渠道上。
func TestBackupReportsFailedChannel(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	dir := t.TempDir()
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"blocked","driver":"local","priority":50,"config":{"path":%q}}`, dir))
	// 系统目录名被同名普通文件占住:快照写不进去;轻探测只查根下一个不存在的键,渠道仍判健康、仍是备份目标。
	if err := os.WriteFile(filepath.Join(dir, "_imgyoung"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := backupChannels(t, inst, admin, csrf)
	resp := adminReq(t, admin, csrf, http.MethodPost, inst.baseURL+"/api/admin/backups", "")
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	named := strings.Contains(string(raw), `\"blocked\"`)
	if err != nil || resp.StatusCode != http.StatusInternalServerError || !named {
		t.Fatalf("立即备份: 状态码 %d err=%v body=%s, 期望 500 且点名渠道 blocked", resp.StatusCode, err, raw)
	}
	var fresh []string
	for name, chans := range backupChannels(t, inst, admin, csrf) {
		if _, ok := before[name]; ok {
			continue
		}
		fresh = append(fresh, name)
		if !slices.Equal(chans, []string{"local"}) {
			t.Fatalf("快照 %s 所在渠道 = %v, 期望只在 local", name, chans)
		}
	}
	if len(fresh) != 1 {
		t.Fatalf("失败的这轮备份新增快照 %v, 期望 local 上新增 1 份", fresh)
	}
}
