//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"testing"
)

// privateUpload 上传一张私有图,返回短 ID 与响应里带签名的访问地址(路径加查询)。
func privateUpload(t *testing.T, inst *instance, fields map[string]string) (id, signedURI string) {
	t.Helper()
	merged := map[string]string{"private": "true"}
	for k, v := range fields {
		merged[k] = v
	}
	resp := upload(t, inst, inst.token, "p.png", pngBytes(t), merged, nil)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		ID        string `json:"id"`
		URL       string `json:"url"`
		Private   bool   `json:"private"`
		ExpiresAt string `json:"expires_at"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil {
		t.Fatalf("私有上传状态码 = %d, 期望 200", resp.StatusCode)
	}
	if !out.Private || out.ExpiresAt == "" {
		t.Fatalf("私有上传响应缺 private 或 expires_at: %+v", out)
	}
	u, err := neturl.Parse(out.URL)
	if err != nil || u.Query().Get("sig") == "" || u.Query().Get("exp") == "" {
		t.Fatalf("私有上传的 url 应带 exp 与 sig: %q", out.URL)
	}
	return out.ID, u.RequestURI()
}

// status 访问一个地址,返回状态码与 Cache-Control。
func status(t *testing.T, inst *instance, uri string) (int, string) {
	t.Helper()
	resp := fetch(t, inst, uri, "")
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Header.Get("Cache-Control")
}

// TestPrivateImages 私有图:只有带有效签名的链接能访问,公开链接、被改动的签名一律 404;
// 签名链接可带处理参数;可改回公开;签发接口只对私有图开放。
func TestPrivateImages(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	id, signed := privateUpload(t, inst, nil)

	if code, cc := status(t, inst, signed); code != http.StatusOK || cc != "private, no-store" {
		t.Errorf("签名链接状态码 = %d, Cache-Control = %q, 期望 200 与 private, no-store", code, cc)
	}
	if code, _ := status(t, inst, "/i/"+id); code != http.StatusNotFound {
		t.Errorf("无签名访问私有图状态码 = %d, 期望 404", code)
	}
	if code, _ := status(t, inst, strings.Replace(signed, "sig=", "sig=x", 1)); code != http.StatusNotFound {
		t.Errorf("签名被改动的状态码 = %d, 期望 404", code)
	}
	if code, _ := status(t, inst, signed+"&w=100"); code != http.StatusOK {
		t.Errorf("签名链接带处理参数的状态码 = %d, 期望 200", code)
	}

	client, csrf := adminSession(t, inst)
	api := inst.baseURL + "/api/admin/images/" + id
	post := func(body string) int {
		resp := adminReq(t, client, csrf, http.MethodPost, api+"/signed-url", body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(`{"ttl_seconds":0}`); code != http.StatusBadRequest {
		t.Errorf("ttl_seconds=0 状态码 = %d, 期望 400", code)
	}
	resp := adminReq(t, client, csrf, http.MethodPost, api+"/signed-url", `{"ttl_seconds":3600}`)
	var link struct {
		URL string `json:"url"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&link) != nil {
		t.Fatalf("签发状态码 = %d, 期望 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
	u, err := neturl.Parse(link.URL)
	if err != nil {
		t.Fatalf("签发的链接: %v", err)
	}
	if code, _ := status(t, inst, u.RequestURI()); code != http.StatusOK {
		t.Errorf("新签发的链接状态码 = %d, 期望 200", code)
	}

	// 后台缩略图走会话鉴权,属主不需要签名。
	thumb := adminReq(t, client, csrf, http.MethodGet, api+"/thumb", "")
	_ = thumb.Body.Close()
	if thumb.StatusCode != http.StatusOK {
		t.Errorf("后台预览私有图状态码 = %d, 期望 200", thumb.StatusCode)
	}

	patch := adminReq(t, client, csrf, http.MethodPatch, api, `{"private":false}`)
	_ = patch.Body.Close()
	if patch.StatusCode != http.StatusOK {
		t.Fatalf("改回公开状态码 = %d, 期望 200", patch.StatusCode)
	}
	if code, _ := status(t, inst, "/i/"+id); code != http.StatusOK {
		t.Errorf("改回公开后无签名访问状态码 = %d, 期望 200", code)
	}
	if code := post(`{"ttl_seconds":3600}`); code != http.StatusConflict {
		t.Errorf("公开图签发状态码 = %d, 期望 409", code)
	}
}

// TestPrivateUploadValidation private 与 signed_ttl 取值非法时 400。
func TestPrivateUploadValidation(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	for name, fields := range map[string]map[string]string{
		"private 非布尔":     {"private": "maybe"},
		"signed_ttl 非整数":  {"private": "true", "signed_ttl": "1h"},
		"signed_ttl 为 0":  {"private": "true", "signed_ttl": "0"},
		"signed_ttl 超过上限": {"private": "true", "signed_ttl": "9999999999999"},
	} {
		resp := upload(t, inst, inst.token, "p.png", pngBytes(t), fields, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s 状态码 = %d, 期望 400", name, resp.StatusCode)
		}
	}
}
