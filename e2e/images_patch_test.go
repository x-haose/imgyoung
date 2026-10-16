//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// imageDetail 图片详情响应中本文件断言用到的字段。
type imageDetail struct {
	ID             string         `json:"id"`              // 引用短 ID
	ExpireAt       *string        `json:"expire_at"`       // 到期时刻;null 表示永久
	TTLSeconds     *int64         `json:"ttl_seconds"`     // 有效时长(秒);null 表示永久
	PendingCleanup bool           `json:"pending_cleanup"` // 是否在待清理列表
	CleanupExempt  bool           `json:"cleanup_exempt"`  // 是否清理豁免
	Replicas       []imageReplica `json:"replicas"`        // 各渠道的副本状态,按渠道优先级降序
}

// imageReplica 详情里一条副本状态。
type imageReplica struct {
	ChannelName string `json:"channel_name"` // 渠道名
	State       string `json:"state"`        // 副本状态
}

// patchImage 以指定会话修改一张图,返回状态码与响应体解析出的详情(非 200 时详情为零值)。
// @param client 带会话 cookie 的客户端
// @param csrf 该会话的 CSRF 令牌
// @param id 引用短 ID
// @param body 请求体 JSON
// @return 状态码
// @return 修改后的详情
func patchImage(t *testing.T, inst *instance, client *http.Client, csrf, id,
	body string) (int, imageDetail) {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodPatch, inst.baseURL+"/api/admin/images/"+id, body)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读修改响应: %v", err)
	}
	var got imageDetail
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("解析修改响应: %v; body=%s", err, raw)
		}
	}
	return resp.StatusCode, got
}

// getImage 以指定会话读图片详情并断言 200。
// @param client 带会话 cookie 的客户端
// @param csrf 该会话的 CSRF 令牌
// @param id 引用短 ID
// @return 详情
func getImage(t *testing.T, inst *instance, client *http.Client, csrf, id string) imageDetail {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/images/"+id, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("图片详情状态码 = %d, 期望 200", resp.StatusCode)
	}
	var got imageDetail
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("解析图片详情: %v", err)
	}
	return got
}

// uploadAs 以指定 Token 上传一张 PNG 并断言 200,返回引用短 ID。
// @param token 上传 Token 明文
// @return 引用短 ID
func uploadAs(t *testing.T, inst *instance, token string) string {
	t.Helper()
	resp := upload(t, inst, token, "mine.png", pngBytes(t), nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("上传状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	return out.ID
}

// TestImagePatchTTLAndExempt 管理员的三种修改各自落库并体现在详情里:
// ttl_hours>0 设到期与时长,ttl_hours=0 改永久,cleanup_exempt 置清理豁免。
func TestImagePatchTTLAndExempt(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	id, _ := mustUpload(t, inst, pngBytes(t), nil, nil)

	code, got := patchImage(t, inst, admin, csrf, id, `{"ttl_hours":2}`)
	if code != http.StatusOK {
		t.Fatalf("设 ttl 状态码 = %d, 期望 200", code)
	}
	if got.ExpireAt == nil || got.TTLSeconds == nil || *got.TTLSeconds != 7200 {
		t.Fatalf("设 ttl 后响应 = %+v, 期望到期有值且时长 7200 秒", got)
	}
	again := getImage(t, inst, admin, csrf, id)
	if again.ExpireAt == nil || *again.ExpireAt != *got.ExpireAt {
		t.Fatalf("详情到期 = %v, 与修改响应 %v 不一致", again.ExpireAt, got.ExpireAt)
	}

	code, got = patchImage(t, inst, admin, csrf, id, `{"ttl_hours":0}`)
	if code != http.StatusOK {
		t.Fatalf("改永久状态码 = %d, 期望 200", code)
	}
	if got.ExpireAt != nil || got.TTLSeconds != nil {
		t.Fatalf("改永久后响应 = %+v, 期望到期与时长均为空", got)
	}
	if again := getImage(t, inst, admin, csrf, id); again.ExpireAt != nil || again.TTLSeconds != nil {
		t.Fatalf("改永久后详情 = %+v, 期望到期与时长均为空", again)
	}

	code, got = patchImage(t, inst, admin, csrf, id, `{"cleanup_exempt":true}`)
	if code != http.StatusOK {
		t.Fatalf("置豁免状态码 = %d, 期望 200", code)
	}
	if !got.CleanupExempt {
		t.Fatalf("置豁免后响应 = %+v, 期望 cleanup_exempt 为真", got)
	}
	if again := getImage(t, inst, admin, csrf, id); !again.CleanupExempt {
		t.Fatalf("置豁免后详情 = %+v, 期望 cleanup_exempt 为真", again)
	}
}

// TestImagePatchPermissions 普通用户只能改自己图的有效期:带豁免项 403,改他人图 404。
func TestImagePatchPermissions(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, adminCSRF := adminSession(t, inst)
	adminImg, _ := mustUpload(t, inst, pngBytes(t), nil, nil)
	createUser(t, inst, admin, adminCSRF, "bob", "bob-secret-1", 0)
	bob, bobCSRF := loginSession(t, inst, "bob", "bob-secret-1")
	_, bobToken := issueToken(t, inst, bob, bobCSRF, "cli")
	bobImg := uploadAs(t, inst, bobToken)

	code, got := patchImage(t, inst, bob, bobCSRF, bobImg, `{"ttl_hours":1}`)
	if code != http.StatusOK || got.TTLSeconds == nil || *got.TTLSeconds != 3600 {
		t.Fatalf("普通用户改自己图的 ttl: 状态码 %d 响应 %+v, 期望 200 且时长 3600 秒", code, got)
	}
	code, _ = patchImage(t, inst, bob, bobCSRF, bobImg, `{"cleanup_exempt":true}`)
	if code != http.StatusForbidden {
		t.Fatalf("普通用户置豁免状态码 = %d, 期望 403", code)
	}
	code, _ = patchImage(t, inst, bob, bobCSRF, adminImg, `{"ttl_hours":1}`)
	if code != http.StatusNotFound {
		t.Fatalf("普通用户改他人图状态码 = %d, 期望 404(不泄露存在性)", code)
	}
	// 被拒的修改不留痕:管理员的图仍是永久。
	if detail := getImage(t, inst, admin, adminCSRF, adminImg); detail.ExpireAt != nil {
		t.Fatalf("他人图被改动: %+v", detail)
	}
}

// TestImagePatchRejectsBadBody 请求体不合法一律 400 且不改动图片;不存在的图 404。
func TestImagePatchRejectsBadBody(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	id, _ := mustUpload(t, inst, pngBytes(t), nil, nil)

	for name, body := range map[string]string{
		"无修改项":    `{}`,
		"ttl 为负":  `{"ttl_hours":-1}`,
		"ttl 超上限": `{"ttl_hours":876001}`,
		"非 JSON":  `not-json`,
	} {
		if code, _ := patchImage(t, inst, admin, csrf, id, body); code != http.StatusBadRequest {
			t.Errorf("%s 状态码 = %d, 期望 400", name, code)
		}
	}
	if detail := getImage(t, inst, admin, csrf, id); detail.ExpireAt != nil || detail.CleanupExempt {
		t.Fatalf("非法请求改动了图片: %+v", detail)
	}
	code, _ := patchImage(t, inst, admin, csrf, "nonexistent0", `{"ttl_hours":1}`)
	if code != http.StatusNotFound {
		t.Fatalf("改不存在的图状态码 = %d, 期望 404", code)
	}
}

// replicaStates 把详情里的副本分布整理成渠道名 → 状态。
func replicaStates(d imageDetail) map[string]string {
	states := make(map[string]string, len(d.Replicas))
	for _, r := range d.Replicas {
		states[r.ChannelName] = r.State
	}
	return states
}

// TestImageDetailReplicas 详情带副本分布:单渠道时只有内置 local 一条就绪副本;加一个渠道后存量回填,
// 两条副本都就绪。
func TestImageDetailReplicas(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	id, _ := mustUpload(t, inst, pngBytes(t), nil, nil)

	if got := replicaStates(getImage(t, inst, admin, csrf, id)); len(got) != 1 || got["local"] != "done" {
		t.Fatalf("单渠道详情副本 = %v, 期望只有 local 一条 done", got)
	}
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"mirror","driver":"local","priority":10,"config":{"path":%q}}`, t.TempDir()))
	waitUntil(t, 15*time.Second, "存量回填到新渠道", func() bool {
		got := replicaStates(getImage(t, inst, admin, csrf, id))
		return len(got) == 2 && got["local"] == "done" && got["mirror"] == "done"
	})
}

// TestImagePatchRejectsExpired 到期是终态:已到期、尚未被清理扫描回收的图不能再延长或改永久(409),
// 访问仍是 404;不涉及有效期的修改照常生效。
func TestImagePatchRejectsExpired(t *testing.T) {
	t.Parallel()
	// 清理扫描间隔拉到 1 小时,测试期间到期的图不会被回收。
	inst := startInstance(t, map[string]any{"cleanup_sweep_seconds": 3600})
	id, url := mustUpload(t, inst, pngBytes(t), map[string]string{"ttl": "1"}, nil)
	waitUntil(t, 10*time.Second, "到期后访问 404", func() bool {
		resp := fetch(t, inst, url, "")
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusNotFound
	})

	admin, csrf := adminSession(t, inst)
	for _, body := range []string{`{"ttl_hours":24}`, `{"ttl_hours":0}`} {
		if code, _ := patchImage(t, inst, admin, csrf, id, body); code != http.StatusConflict {
			t.Fatalf("已到期的图修改有效期 %s 状态码 = %d, 期望 409", body, code)
		}
	}
	resp := fetch(t, inst, url, "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("修改被拒后访问状态码 = %d, 期望仍为 404", resp.StatusCode)
	}
	if code, got := patchImage(t, inst, admin, csrf, id, `{"cleanup_exempt":true}`); code != http.StatusOK ||
		!got.CleanupExempt {
		t.Fatalf("已到期的图设置清理豁免: 状态码 %d, cleanup_exempt=%v, 期望 200 且已豁免", code, got.CleanupExempt)
	}
}
