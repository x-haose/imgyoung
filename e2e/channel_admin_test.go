//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// channelInfo 渠道描述响应的断言用投影。
type channelInfo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Driver        string `json:"driver"`
	Priority      int64  `json:"priority"`
	Healthy       bool   `json:"healthy"`
	LastCheckedAt string `json:"last_checked_at"`
	LastError     string `json:"last_error"`
	Replicas      struct {
		Done    int64 `json:"done"`
		Pending int64 `json:"pending"`
		Failed  int64 `json:"failed"`
	} `json:"replicas"`
}

// listChannels 以管理员会话拉取渠道列表。
func listChannels(t *testing.T, inst *instance) []channelInfo {
	t.Helper()
	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/channels", "")
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Items []channelInfo `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析渠道列表: %v", err)
	}
	return out.Items
}

// channelHealthy 判断指定渠道当前是否健康;不存在视为不健康。
func channelHealthy(t *testing.T, inst *instance, name string) bool {
	t.Helper()
	for _, c := range listChannels(t, inst) {
		if c.Name == name {
			return c.Healthy
		}
	}
	return false
}

// decodeChannelInfo 解析渠道描述响应并断言状态码。
func decodeChannelInfo(t *testing.T, resp *http.Response, wantCode int) channelInfo {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantCode {
		t.Fatalf("状态码 = %d, 期望 %d; body=%s", resp.StatusCode, wantCode, raw)
	}
	var info channelInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatalf("解析渠道描述: %v; body=%s", err, raw)
	}
	return info
}

// TestChannelListEnvelope 渠道列表走 {items,total} 信封、含新建渠道,且不下发配置与凭证。
func TestChannelListEnvelope(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"backup","driver":"local","priority":10,"config":{"path":%q}}`, t.TempDir()))

	client, csrf := adminSession(t, inst)
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/channels", "")
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取列表响应: %v", err)
	}
	var lr struct {
		Items []channelInfo `json:"items"`
		Total int64         `json:"total"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("列表响应非 {items,total} 信封: %v; body=%s", err, raw)
	}
	hasBackup := false
	for _, c := range lr.Items {
		if c.Name == "backup" {
			hasBackup = true
		}
	}
	if lr.Total < 2 || int(lr.Total) != len(lr.Items) || !hasBackup {
		t.Fatalf("渠道列表信封异常(应含 backup 且 total 与 items 一致): %s", raw)
	}
	if bytes.Contains(raw, []byte("config")) {
		t.Fatalf("渠道列表泄露配置字段: %s", raw)
	}
}

// TestChannelCreateValidation 创建接口的输入校验:未知字段与「加密 + 直链」矛盾组合一律 400。
func TestChannelCreateValidation(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)

	unknown := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels",
		`{"name":"x","driver":"local","prioritty":5}`)
	code := unknown.StatusCode
	_ = unknown.Body.Close()
	if code != http.StatusBadRequest {
		t.Fatalf("未知字段状态码 = %d, 期望 400", code)
	}
	conflict := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels",
		fmt.Sprintf(`{"name":"y","driver":"local","encrypted":true,"allow_direct_link":true,"config":{"path":%q}}`,
			t.TempDir()))
	code = conflict.StatusCode
	_ = conflict.Body.Close()
	if code != http.StatusBadRequest {
		t.Fatalf("加密+直链矛盾组合状态码 = %d, 期望 400", code)
	}
}

// TestChannelUpdate 编辑渠道:改名改优先级即返回最新描述(含编辑即探测的结果),
// 新优先级立即生效于选路;encrypted 翻转 409;渠道不存在 404。
func TestChannelUpdate(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	dir := t.TempDir()
	id := createChannel(t, inst, fmt.Sprintf(
		`{"name":"editable","driver":"local","priority":10,"config":{"path":%q}}`, dir))
	url := fmt.Sprintf("%s/api/admin/channels/%d", inst.baseURL, id)

	updated := adminReq(t, client, csrf, http.MethodPut, url,
		fmt.Sprintf(`{"name":"edited","driver":"local","priority":500,"config":{"path":%q}}`, dir))
	info := decodeChannelInfo(t, updated, http.StatusOK)
	if info.Name != "edited" || info.Priority != 500 || !info.Healthy || info.LastCheckedAt == "" {
		t.Fatalf("编辑响应异常: %+v", info)
	}

	// 新优先级(500 > 内置 100)使该渠道成为主渠道,上传同步落其目录。
	_, _ = mustUpload(t, inst, pngBytes(t), nil, nil)
	if countObjects(dir) == 0 {
		t.Fatal("上传未落到编辑后的主渠道目录")
	}

	flip := adminReq(t, client, csrf, http.MethodPut, url,
		fmt.Sprintf(`{"name":"edited","driver":"local","priority":500,"encrypted":true,"config":{"path":%q}}`, dir))
	code := flip.StatusCode
	_ = flip.Body.Close()
	if code != http.StatusConflict {
		t.Fatalf("翻转加密属性状态码 = %d, 期望 409", code)
	}
	missing := adminReq(t, client, csrf, http.MethodPut, inst.baseURL+"/api/admin/channels/99999",
		`{"name":"x","driver":"local"}`)
	code = missing.StatusCode
	_ = missing.Body.Close()
	if code != http.StatusNotFound {
		t.Fatalf("编辑不存在渠道状态码 = %d, 期望 404", code)
	}
}

// testConnectionResult 测试连接响应的投影。
type testConnectionResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// testConnection 发一次测试连接请求,返回解析结果与原始响应体。
func testConnection(t *testing.T, inst *instance, client *http.Client, csrf, body string,
) (testConnectionResult, string) {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels/test", body)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("测试连接状态码 = %d, 期望 200(结果在信封里): %s", resp.StatusCode, raw)
	}
	var out testConnectionResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析测试连接响应: %v; body=%s", err, raw)
	}
	return out, string(raw)
}

// TestChannelTestAndCheck 测试连接:好配置 ok=true、坏配置 ok=false 且不回显凭证、都不留渠道记录;
// 手动重检返回探测后的完整描述(含时刻与副本水位),渠道不存在 404。
func TestChannelTestAndCheck(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)

	good, _ := testConnection(t, inst, client, csrf,
		fmt.Sprintf(`{"name":"probe","driver":"local","config":{"path":%q}}`, t.TempDir()))
	if !good.OK || good.Error != "" {
		t.Fatalf("好配置结果 = %+v, 期望 ok", good)
	}
	const secretValue = "TESTSECRETNEVERSHOWN"
	bad, raw := testConnection(t, inst, client, csrf,
		`{"name":"probe","driver":"s3","config":{"provider":"Minio","endpoint":"http://127.0.0.1:1",`+
			`"access_key_id":"AK","secret_access_key":"`+secretValue+`","path":"bkt"}}`)
	if bad.OK || bad.Error == "" {
		t.Fatalf("坏配置结果 = %+v, 期望 ok=false 且带错误文案", bad)
	}
	if strings.Contains(raw, secretValue) {
		t.Fatalf("测试连接响应回显了凭证: %s", raw)
	}
	if n := len(listChannels(t, inst)); n != 1 {
		t.Fatalf("测试连接后渠道数 = %d, 期望仍只有内置 local", n)
	}

	// 上传一张图,让内置 local 渠道拥有一份 done 副本,重检的副本水位才有内容可断言。
	_, _ = mustUpload(t, inst, pngBytes(t), nil, nil)
	checked := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels/1/check", "")
	info := decodeChannelInfo(t, checked, http.StatusOK)
	if !info.Healthy || info.LastCheckedAt == "" || info.LastError != "" {
		t.Fatalf("重检结果异常: %+v", info)
	}
	if info.Replicas.Done != 1 || info.Replicas.Pending != 0 || info.Replicas.Failed != 0 {
		t.Fatalf("副本水位 = %+v, 期望 done=1", info.Replicas)
	}
	missing := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels/99999/check", "")
	code := missing.StatusCode
	_ = missing.Body.Close()
	if code != http.StatusNotFound {
		t.Fatalf("重检不存在渠道状态码 = %d, 期望 404", code)
	}
}
