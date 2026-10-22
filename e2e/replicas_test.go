//go:build e2e

// 渠道迁移相关的 e2e:渠道副本清单、立即重试失败的副本、补齐带宽上限配置。
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// replicaPage 副本清单响应中本文件断言用到的字段。
type replicaPage struct {
	Items []struct {
		ImageID    *string `json:"image_id"`    // 引用该物理图的最早一张图片
		ImageCount int64   `json:"image_count"` // 引用张数
		State      string  `json:"state"`       // 副本状态
		Failure    *struct {
			Reason string `json:"reason"` // 失败归类
			Detail string `json:"detail"` // 原文
		} `json:"failure"` // 最近一次失败
	} `json:"items"` // 当前页
	Total int64 `json:"total"` // 过滤后的总数
}

// listReplicas 列出渠道副本并断言 200。
func listReplicas(t *testing.T, inst *instance, channelID int64, query string) replicaPage {
	t.Helper()
	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodGet,
		fmt.Sprintf("%s/api/admin/channels/%d/replicas%s", inst.baseURL, channelID, query), "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("副本清单状态码 = %d, 期望 200", resp.StatusCode)
	}
	var page replicaPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("解析副本清单: %v", err)
	}
	return page
}

// TestChannelReplicaListAndRetry 新渠道补齐完成后清单里是就绪副本;把它标成失败后能按状态查到失败原因与图片;
// 立即重试把它改回待补并最终补齐;非法参数与不存在的渠道被拒绝。
func TestChannelReplicaListAndRetry(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	inst := startInstance(t, nil)
	channelID := createChannel(t, inst, fmt.Sprintf(
		`{"name":"backup","driver":"local","priority":300,"config":{"path":%q}}`, t.TempDir()))
	imageID, _ := mustUpload(t, inst, pngBytes(t), nil, nil)
	waitUntil(t, 15*time.Second, "副本在新渠道上就绪", func() bool {
		return listReplicas(t, inst, channelID, "?state=done").Total == 1
	})

	execInstanceDB(t, inst, `UPDATE blob_channels
SET state = 'failed', last_error_reason = 'timeout', last_error = 'boom' WHERE channel_id = ?`, channelID)
	failed := listReplicas(t, inst, channelID, "?state=failed")
	if failed.Total != 1 || len(failed.Items) != 1 || failed.Items[0].State != "failed" ||
		failed.Items[0].Failure == nil || failed.Items[0].Failure.Reason != "timeout" ||
		failed.Items[0].ImageID == nil || *failed.Items[0].ImageID != imageID || failed.Items[0].ImageCount != 1 {
		t.Fatalf("失败清单 = %+v, 期望带失败原因与上传的图片 %s", failed, imageID)
	}

	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodPost,
		fmt.Sprintf("%s/api/admin/channels/%d/replicas/retry", inst.baseURL, channelID), "")
	var retry struct {
		Retried int64 `json:"retried"` // 改回待补的副本数
		Queued  bool  `json:"queued"`  // 是否已全部入队
	}
	err := json.NewDecoder(resp.Body).Decode(&retry)
	if err != nil || resp.StatusCode != http.StatusOK || retry.Retried != 1 || !retry.Queued {
		t.Fatalf("重试 = %d %+v err=%v, 期望改回 1 个且已入队", resp.StatusCode, retry, err)
	}
	_ = resp.Body.Close()
	waitUntil(t, 15*time.Second, "失败的副本重新补齐", func() bool {
		return listReplicas(t, inst, channelID, "?state=done").Total == 1
	})

	for path, want := range map[string]int{
		fmt.Sprintf("/api/admin/channels/%d/replicas?state=bogus", channelID): http.StatusBadRequest,
		"/api/admin/channels/99999/replicas":                                  http.StatusNotFound,
	} {
		resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+path, "")
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("GET %s 状态码 = %d, 期望 %d", path, resp.StatusCode, want)
		}
	}
	resp = adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels/99999/replicas/retry", "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("重试不存在的渠道状态码 = %d, 期望 404", resp.StatusCode)
	}
}

// TestReplicateLimitSetting 补齐带宽上限:默认 0(不限),可以设置并立即读回;负数被拒绝。
func TestReplicateLimitSetting(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)

	read := func() int {
		t.Helper()
		resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/config/full", "")
		defer func() { _ = resp.Body.Close() }()
		var cfg struct {
			Settings struct {
				Limit int `json:"replicate_limit_mbps"` // 补齐带宽上限
			} `json:"settings"` // 运营配置
		}
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
			t.Fatalf("解析完整配置: %v", err)
		}
		return cfg.Settings.Limit
	}
	if got := read(); got != 0 {
		t.Fatalf("默认值 = %d, 期望 0(不限)", got)
	}
	configure(t, inst, map[string]any{"replicate_limit_mbps": 5})
	if got := read(); got != 5 {
		t.Fatalf("设置后 = %d, 期望 5", got)
	}
	resp := adminReq(t, client, csrf, http.MethodPut, inst.baseURL+"/api/admin/config", `{"replicate_limit_mbps":-1}`)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("负数状态码 = %d, 期望 400", resp.StatusCode)
	}
}
