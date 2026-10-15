//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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

// listFiles 列出目录下全部文件相对目录的路径(含系统目录下的),按字典序。
// @param dir 渠道存储目录
// @return 相对路径列表;目录不存在为空
func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("遍历 %s: %v", dir, err)
	}
	slices.Sort(out)
	return out
}

// TestChannelDeletePurgesObjects 删除渠道的两条路径:带清理的删除异步删光本服务写在渠道上的对象
// (图片副本、元数据库快照与深探测残留)后渠道消失,别的程序写的文件不动,另一渠道的副本仍可访问;
// 不清理的删除立即消失、远端对象(含快照)原样保留;不存在的渠道 404。
func TestChannelDeletePurgesObjects(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
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
	createBackup(t, inst, admin, csrf)
	for name, content := range map[string]string{".imgyoung-probe-0123456789abcdef": "probe", "notes.txt": "mine"} {
		if err := os.WriteFile(filepath.Join(purgeDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if code := deleteChannel(t, inst, 99999, true); code != http.StatusNotFound {
		t.Fatalf("删除不存在的渠道状态码 = %d, 期望 404", code)
	}
	if code := deleteChannel(t, inst, purgeID, true); code != http.StatusAccepted {
		t.Fatalf("带清理删除状态码 = %d, 期望 202", code)
	}
	waitUntil(t, 15*time.Second, "渠道清理完毕并移除", func() bool { return !channelListed(t, inst, purgeID) })
	if got := listFiles(t, purgeDir); !slices.Equal(got, []string{"notes.txt"}) {
		t.Fatalf("清理后渠道目录剩 %v, 期望只剩别的程序写的 notes.txt", got)
	}
	if got := fetchBytes(t, inst, url); !bytes.Equal(got, payload) {
		t.Fatal("被删渠道清理后,另一渠道的副本应仍可访问")
	}

	keepDir := t.TempDir()
	keepID := createChannel(t, inst, fmt.Sprintf(
		`{"name":"keep-objects","driver":"local","priority":300,"config":{"path":%q}}`, keepDir))
	_, _ = mustUpload(t, inst, payload, nil, nil)
	waitUntil(t, 15*time.Second, "新上传补齐到 local", func() bool { return countObjects(localDir) >= 2 })
	snap := createBackup(t, inst, admin, csrf)
	if code := deleteChannel(t, inst, keepID, false); code != http.StatusNoContent {
		t.Fatalf("不清理删除状态码 = %d, 期望 204", code)
	}
	if channelListed(t, inst, keepID) {
		t.Fatal("不清理的删除应立即移除渠道")
	}
	if countObjects(keepDir) == 0 || !slices.Contains(listFiles(t, keepDir), "_imgyoung/backup/"+snap.Name) {
		t.Fatalf("不清理的删除不应触碰远端对象,渠道目录现为 %v", listFiles(t, keepDir))
	}
}
