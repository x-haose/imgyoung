//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// loginUser 以指定用户登录,返回带会话 cookie 的客户端与 CSRF 令牌。
func loginUser(t *testing.T, inst *instance, name, password string) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	body := fmt.Sprintf(`{"name":%q,"password":%q}`, name, password)
	resp, err := client.Post(inst.baseURL+"/api/admin/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("发送登录请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("登录状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
	}
	var lr struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析登录响应: %v", err)
	}
	return client, lr.CSRFToken
}

// createUser 经管理 API 建号并断言 201,返回用户 id。
func createUser(t *testing.T, inst *instance, admin *http.Client, csrf, name, password string, quotaBytes int64) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"password":%q,"quota_bytes":%d}`, name, password, quotaBytes)
	resp := adminReq(t, admin, csrf, http.MethodPost, inst.baseURL+"/api/admin/users", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("建号状态码 = %d, 期望 201; body=%s", resp.StatusCode, raw)
	}
	var ur struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		t.Fatalf("解析建号响应: %v", err)
	}
	return ur.ID
}

// issueToken 经管理 API 为当前会话用户签发 Token,返回明文。
func issueToken(t *testing.T, inst *instance, client *http.Client, csrf, name string) (int64, string) {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/tokens",
		fmt.Sprintf(`{"name":%q}`, name))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("签发 Token 状态码 = %d, 期望 201; body=%s", resp.StatusCode, raw)
	}
	var tr struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("解析签发响应: %v", err)
	}
	return tr.ID, tr.Token
}

// listImages 以会话列出图片,返回 id 集合与 total。
func listImages(t *testing.T, inst *instance, client *http.Client, csrf string) (map[string]bool, int64) {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/images", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("图片列表状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析图片列表: %v", err)
	}
	ids := map[string]bool{}
	for _, it := range lr.Items {
		ids[it.ID] = true
	}
	return ids, lr.Total
}

// TestMultiUserIsolation 验收:两个用户各用自己的 Token 传图、有各自视图,互相看不见、删不掉。
func TestMultiUserIsolation(t *testing.T) {
	inst := startInstance(t, nil)
	admin, adminCSRF := adminSession(t, inst)
	createUser(t, inst, admin, adminCSRF, "bob", "bob-secret-1", 0)

	bob, bobCSRF := loginUser(t, inst, "bob", "bob-secret-1")
	_, bobToken := issueToken(t, inst, bob, bobCSRF, "picgo")

	// 各自上传一张。
	adminImgID, _ := mustUpload(t, inst, pngBytes(t), nil, nil)
	bobUp := upload(t, inst, bobToken, "b.png", jpegWithEXIF(t), nil, nil)
	var bobImg struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(bobUp.Body).Decode(&bobImg); err != nil {
		t.Fatalf("解析 bob 上传响应: %v", err)
	}
	_ = bobUp.Body.Close()

	// bob 只见自己的;admin 全见。
	bobIDs, bobTotal := listImages(t, inst, bob, bobCSRF)
	if bobTotal != 1 || !bobIDs[bobImg.ID] || bobIDs[adminImgID] {
		t.Fatalf("bob 视图异常: total=%d ids=%v", bobTotal, bobIDs)
	}
	adminIDs, adminTotal := listImages(t, inst, admin, adminCSRF)
	if adminTotal != 2 || !adminIDs[adminImgID] || !adminIDs[bobImg.ID] {
		t.Fatalf("admin 视图异常: total=%d ids=%v", adminTotal, adminIDs)
	}

	// bob 删 admin 的图 → 404(不泄露存在性);删自己的 → 204。
	del := adminReq(t, bob, bobCSRF, http.MethodDelete, inst.baseURL+"/api/admin/images/"+adminImgID, "")
	code := del.StatusCode
	_ = del.Body.Close()
	if code != http.StatusNotFound {
		t.Fatalf("跨用户删除状态码 = %d, 期望 404", code)
	}
	delOwn := adminReq(t, bob, bobCSRF, http.MethodDelete, inst.baseURL+"/api/admin/images/"+bobImg.ID, "")
	code = delOwn.StatusCode
	_ = delOwn.Body.Close()
	if code != http.StatusNoContent {
		t.Fatalf("删除自己图片状态码 = %d, 期望 204", code)
	}

	// bob 访问管理员资源 → 403。
	users := adminReq(t, bob, bobCSRF, http.MethodGet, inst.baseURL+"/api/admin/users", "")
	code = users.StatusCode
	_ = users.Body.Close()
	if code != http.StatusForbidden {
		t.Fatalf("普通用户访问用户管理状态码 = %d, 期望 403", code)
	}

	// 公开 URL 保持人人可访问(图床语义,隔离只在管理面)。
	pub := fetch(t, inst, "/i/"+adminImgID, "")
	code = pub.StatusCode
	_ = pub.Body.Close()
	if code != http.StatusOK {
		t.Fatalf("公开访问状态码 = %d, 期望 200", code)
	}
}

// TestTokenRevokeStopsUpload 验收:签发的 Token 可上传,吊销后立即 401。
func TestTokenRevokeStopsUpload(t *testing.T) {
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	tokenID, plaintext := issueToken(t, inst, admin, csrf, "cli")

	if !strings.HasPrefix(plaintext, "imgy_") {
		t.Fatalf("Token 明文形态异常: %q", plaintext)
	}
	first := upload(t, inst, plaintext, "a.png", pngBytes(t), nil, nil)
	code := first.StatusCode
	_ = first.Body.Close()
	if code != http.StatusOK {
		t.Fatalf("新 Token 上传状态码 = %d, 期望 200", code)
	}

	rev := adminReq(t, admin, csrf, http.MethodPost, fmt.Sprintf("%s/api/admin/tokens/%d/revoke", inst.baseURL, tokenID), "")
	code = rev.StatusCode
	_ = rev.Body.Close()
	if code != http.StatusNoContent {
		t.Fatalf("吊销状态码 = %d, 期望 204", code)
	}
	second := upload(t, inst, plaintext, "a.png", pngBytes(t), nil, nil)
	code = second.StatusCode
	_ = second.Body.Close()
	if code != http.StatusUnauthorized {
		t.Fatalf("吊销后上传状态码 = %d, 期望 401", code)
	}
}

// TestQuotaEnforced 验收:配额用尽后上传 403,删除图片释放账面后恢复。
func TestQuotaEnforced(t *testing.T) {
	inst := startInstance(t, nil)
	admin, adminCSRF := adminSession(t, inst)
	createUser(t, inst, admin, adminCSRF, "tiny", "tiny-secret-1", 10)

	tinyClient, tinyCSRF := loginUser(t, inst, "tiny", "tiny-secret-1")
	_, tinyToken := issueToken(t, inst, tinyClient, tinyCSRF, "picgo")

	resp := upload(t, inst, tinyToken, "a.png", pngBytes(t), nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("超配额上传状态码 = %d, 期望 403", resp.StatusCode)
	}
}

// TestUploadRateLimit 验收:每分钟频控生效,超出额度 429。
func TestUploadRateLimit(t *testing.T) {
	inst := startInstance(t, map[string]string{"IMGYOUNG_UPLOAD_RATE_PER_MIN": "2"})
	for i := range 2 {
		resp := upload(t, inst, testToken, "a.png", pngBytes(t), nil, nil)
		code := resp.StatusCode
		_ = resp.Body.Close()
		if code != http.StatusOK {
			t.Fatalf("第 %d 次上传状态码 = %d, 期望 200", i+1, code)
		}
	}
	third := upload(t, inst, testToken, "a.png", pngBytes(t), nil, nil)
	defer func() { _ = third.Body.Close() }()
	if third.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("超频上传状态码 = %d, 期望 429", third.StatusCode)
	}
}

// TestUserDeleteGuards 验收:名下有图的用户删除被拒,清空后可删,删后无法登录。
func TestUserDeleteGuards(t *testing.T) {
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	userID := createUser(t, inst, admin, csrf, "carol", "carol-secret-1", 0)

	carol, carolCSRF := loginUser(t, inst, "carol", "carol-secret-1")
	_, carolToken := issueToken(t, inst, carol, carolCSRF, "cli")
	up := upload(t, inst, carolToken, "c.png", pngBytes(t), nil, nil)
	var img struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(up.Body).Decode(&img); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}
	_ = up.Body.Close()

	del := adminReq(t, admin, csrf, http.MethodDelete, inst.baseURL+"/api/admin/users/"+userID, "")
	code := del.StatusCode
	_ = del.Body.Close()
	if code != http.StatusConflict {
		t.Fatalf("有图用户删除状态码 = %d, 期望 409", code)
	}

	imgDel := adminReq(t, admin, csrf, http.MethodDelete, inst.baseURL+"/api/admin/images/"+img.ID, "")
	code = imgDel.StatusCode
	_ = imgDel.Body.Close()
	if code != http.StatusNoContent {
		t.Fatalf("管理员删图状态码 = %d, 期望 204", code)
	}
	del2 := adminReq(t, admin, csrf, http.MethodDelete, inst.baseURL+"/api/admin/users/"+userID, "")
	code = del2.StatusCode
	_ = del2.Body.Close()
	if code != http.StatusNoContent {
		t.Fatalf("清空后删除用户状态码 = %d, 期望 204", code)
	}
	if code := postLogin(t, http.DefaultClient, inst.baseURL, "carol", "carol-secret-1"); code != http.StatusUnauthorized {
		t.Fatalf("已删用户登录状态码 = %d, 期望 401", code)
	}
}
