//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// deleteChannel 通过管理 API 删除渠道,返回状态码。
// @param purge 是否要求清理渠道上的对象
func deleteChannel(t *testing.T, inst *instance, id int64, purge bool) int {
	t.Helper()
	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodDelete,
		fmt.Sprintf("%s/api/admin/channels/%d?purge=%v", inst.baseURL, id, purge), "")
	code := resp.StatusCode
	_ = resp.Body.Close()
	return code
}

// channelListed 渠道是否仍出现在渠道列表中。
func channelListed(t *testing.T, inst *instance, id int64) bool {
	t.Helper()
	for _, c := range listChannels(t, inst) {
		if c.ID == id {
			return true
		}
	}
	return false
}

// TestChannelDeletePurgesObjects 删除渠道的两条路径:带清理的删除异步删光渠道上的对象后
// 渠道消失,另一渠道的副本仍可访问;不清理的删除立即消失、远端对象原样保留;不存在的渠道 404。
func TestChannelDeletePurgesObjects(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	purgeDir := t.TempDir()
	purgeID := createChannel(t, inst, fmt.Sprintf(
		`{"name":"purge-me","driver":"local","priority":300,"config":{"path":%q}}`, purgeDir))
	payload := pngBytes(t)
	_, url := mustUpload(t, inst, payload, nil, nil)
	localDir := filepath.Join(inst.dataDir, "blobs")
	waitUntil(t, 15*time.Second, "副本补齐到 local", func() bool { return countObjects(localDir) > 0 })
	if countObjects(purgeDir) == 0 {
		t.Fatal("主渠道没有对象")
	}

	if code := deleteChannel(t, inst, 99999, true); code != http.StatusNotFound {
		t.Fatalf("删除不存在的渠道状态码 = %d, 期望 404", code)
	}
	if code := deleteChannel(t, inst, purgeID, true); code != http.StatusAccepted {
		t.Fatalf("带清理删除状态码 = %d, 期望 202", code)
	}
	waitUntil(t, 15*time.Second, "渠道对象清空并移除渠道", func() bool {
		return countObjects(purgeDir) == 0 && !channelListed(t, inst, purgeID)
	})
	if got := fetchBytes(t, inst, url); !bytes.Equal(got, payload) {
		t.Fatal("被删渠道清理后,另一渠道的副本应仍可访问")
	}

	keepDir := t.TempDir()
	keepID := createChannel(t, inst, fmt.Sprintf(
		`{"name":"keep-objects","driver":"local","priority":300,"config":{"path":%q}}`, keepDir))
	_, _ = mustUpload(t, inst, payload, nil, nil)
	waitUntil(t, 15*time.Second, "新上传补齐到 local", func() bool { return countObjects(localDir) >= 2 })
	if code := deleteChannel(t, inst, keepID, false); code != http.StatusNoContent {
		t.Fatalf("不清理删除状态码 = %d, 期望 204", code)
	}
	if channelListed(t, inst, keepID) {
		t.Fatal("不清理的删除应立即移除渠道")
	}
	if countObjects(keepDir) == 0 {
		t.Fatal("不清理的删除不应触碰远端对象")
	}
}
