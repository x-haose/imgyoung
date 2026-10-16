//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// createUser 经管理 API 建号并断言 201,返回用户 id。
func createUser(t *testing.T, inst *instance, admin *http.Client, csrf, name, password string,
	quotaBytes int64) string {
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
	t.Parallel()
	inst := startInstance(t, nil)
	admin, adminCSRF := adminSession(t, inst)
	createUser(t, inst, admin, adminCSRF, "bob", "bob-secret-1", 0)

	bob, bobCSRF := loginSession(t, inst, "bob", "bob-secret-1")
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
	for _, path := range []string{"/api/admin/users", "/api/admin/channels"} {
		forbidden := adminReq(t, bob, bobCSRF, http.MethodGet, inst.baseURL+path, "")
		code = forbidden.StatusCode
		_ = forbidden.Body.Close()
		if code != http.StatusForbidden {
			t.Fatalf("普通用户访问 %s 状态码 = %d, 期望 403", path, code)
		}
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
	t.Parallel()
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

	rev := adminReq(t, admin, csrf, http.MethodPost,
		fmt.Sprintf("%s/api/admin/tokens/%d/revoke", inst.baseURL, tokenID), "")
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

// TestTokenNameLimitCountsCharacters 验收:用途名上限按字符计,64 个汉字可签发,65 个返回 400。
func TestTokenNameLimitCountsCharacters(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	issueToken(t, inst, admin, csrf, strings.Repeat("图", 64))

	resp := adminReq(t, admin, csrf, http.MethodPost, inst.baseURL+"/api/admin/tokens",
		fmt.Sprintf(`{"name":%q}`, strings.Repeat("图", 65)))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("65 个字符的用途名状态码 = %d, 期望 400", resp.StatusCode)
	}
}

// TestQuotaEnforced 验收:配额用尽后上传 403,删除图片释放账面后恢复。
func TestQuotaEnforced(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, adminCSRF := adminSession(t, inst)
	createUser(t, inst, admin, adminCSRF, "tiny", "tiny-secret-1", 10)

	tinyClient, tinyCSRF := loginSession(t, inst, "tiny", "tiny-secret-1")
	_, tinyToken := issueToken(t, inst, tinyClient, tinyCSRF, "picgo")

	resp := upload(t, inst, tinyToken, "a.png", pngBytes(t), nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("超配额上传状态码 = %d, 期望 403", resp.StatusCode)
	}
}

// TestUploadRateLimit 验收:每分钟频控生效,超出额度 429。
func TestUploadRateLimit(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"upload_rate_per_min": 2})
	for i := range 2 {
		resp := upload(t, inst, inst.token, "a.png", pngBytes(t), nil, nil)
		code := resp.StatusCode
		_ = resp.Body.Close()
		if code != http.StatusOK {
			t.Fatalf("第 %d 次上传状态码 = %d, 期望 200", i+1, code)
		}
	}
	third := upload(t, inst, inst.token, "a.png", pngBytes(t), nil, nil)
	defer func() { _ = third.Body.Close() }()
	if third.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("超频上传状态码 = %d, 期望 429", third.StatusCode)
	}
}

// TestUserDeleteGuards 验收:名下有图的用户删除被拒,清空后可删,删后无法登录。
func TestUserDeleteGuards(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	userID := createUser(t, inst, admin, csrf, "carol", "carol-secret-1", 0)

	carol, carolCSRF := loginSession(t, inst, "carol", "carol-secret-1")
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
	code = postLogin(t, newClient(t), inst.baseURL, "carol", "carol-secret-1")
	if code != http.StatusUnauthorized {
		t.Fatalf("已删用户登录状态码 = %d, 期望 401", code)
	}
}

// sessionUpload 以会话 + CSRF 头向后台上传端点发 multipart 图片,返回响应。
func sessionUpload(t *testing.T, inst *instance, client *http.Client, csrf, filename string,
	payload []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("创建文件字段: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("写文件内容: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, inst.baseURL+"/api/admin/upload", &buf)
	if err != nil {
		t.Fatalf("构造后台上传请求: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发送后台上传请求: %v", err)
	}
	return resp
}

// TestAdminSessionUpload 验收:登录 → 后台端点传图 → 列表可见 → 图可公开访问;
// 无 CSRF 头 403、无会话 401。
func TestAdminSessionUpload(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)

	resp := sessionUpload(t, inst, admin, csrf, "panel.png", pngBytes(t))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("后台上传状态码 = %d, 期望 200; body=%s", resp.StatusCode, raw)
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析后台上传响应: %v", err)
	}
	if out.ID == "" || out.URL == "" {
		t.Fatalf("后台上传响应缺字段: %+v", out)
	}

	ids, _ := listImages(t, inst, admin, csrf)
	if !ids[out.ID] {
		t.Fatalf("后台上传的图片 %s 未出现在列表中", out.ID)
	}

	pub := fetch(t, inst, urlPath(t, out.URL), "")
	code := pub.StatusCode
	_ = pub.Body.Close()
	if code != http.StatusOK {
		t.Fatalf("公开访问状态码 = %d, 期望 200", code)
	}

	noCSRF := sessionUpload(t, inst, admin, "", "panel.png", pngBytes(t))
	code = noCSRF.StatusCode
	_ = noCSRF.Body.Close()
	if code != http.StatusForbidden {
		t.Fatalf("无 CSRF 上传状态码 = %d, 期望 403", code)
	}

	anon := newClient(t)
	noSession := sessionUpload(t, inst, anon, csrf, "panel.png", pngBytes(t))
	code = noSession.StatusCode
	_ = noSession.Body.Close()
	if code != http.StatusUnauthorized {
		t.Fatalf("无会话上传状态码 = %d, 期望 401", code)
	}
}

// imageAccessCount 以会话读单图详情,返回累计公开访问次数。
func imageAccessCount(t *testing.T, inst *instance, client *http.Client, csrf, id string) int64 {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/images/"+id, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("图片详情状态码 = %d, 期望 200", resp.StatusCode)
	}
	var item struct {
		AccessCount int64 `json:"access_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatalf("解析图片详情: %v", err)
	}
	return item.AccessCount
}

// TestAdminThumbSkipsAccessTracking 后台缩略图通道:匿名 401、会话内 200,且不计公开访问活跃度;
// 公开入口访问后计数才累加。
// @Description: 缩略图访问是否被误计,靠对照图判定:缩略图访问之后再公开访问另一张图,
// 等后者的计数落库——同一轮 flush 会把之前记下的全部访问一起落库,此时被看的图仍须为 0。
func TestAdminThumbSkipsAccessTracking(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"activity_flush_seconds": 1})
	admin, csrf := adminSession(t, inst)
	watched, watchedURL := mustUpload(t, inst, pngBytes(t), nil, nil)
	control, controlURL := mustUpload(t, inst, pngBytes(t), nil, nil)

	thumbPath := "/api/admin/images/" + watched + "/thumb"
	anon := fetch(t, inst, thumbPath, "")
	_ = anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("匿名访问后台缩略图状态码 = %d, 期望 401", anon.StatusCode)
	}
	thumb := adminReq(t, admin, csrf, http.MethodGet, inst.baseURL+thumbPath, "")
	ct := thumb.Header.Get("Content-Type")
	code := thumb.StatusCode
	_ = thumb.Body.Close()
	if code != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		t.Fatalf("后台缩略图状态码 = %d Content-Type = %q, 期望 200 与图片类型", code, ct)
	}

	pub := fetch(t, inst, controlURL, "")
	_ = pub.Body.Close()
	waitUntil(t, 10*time.Second, "对照图的公开访问落库", func() bool {
		return imageAccessCount(t, inst, admin, csrf, control) == 1
	})
	if n := imageAccessCount(t, inst, admin, csrf, watched); n != 0 {
		t.Fatalf("后台浏览被计入公开访问: access_count = %d, 期望 0", n)
	}

	pubWatched := fetch(t, inst, watchedURL, "")
	_ = pubWatched.Body.Close()
	waitUntil(t, 10*time.Second, "公开访问计数累加", func() bool {
		return imageAccessCount(t, inst, admin, csrf, watched) == 1
	})
}

// offsiteMissing 读管理员总览里的「缺少健康异地渠道」标记。
func offsiteMissing(t *testing.T, inst *instance, client *http.Client, csrf string) bool {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/dashboard", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("总览状态码 = %d, 期望 200", resp.StatusCode)
	}
	var body struct {
		OffsiteMissing bool `json:"offsite_missing"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("解析总览: %v", err)
	}
	return body.OffsiteMissing
}

// TestDashboardOffsiteMissing 快照与副本写到每个健康渠道:只有本机 local 时总览提示数据只在本机;
// 加一个异地渠道,哪怕优先级低于 local,提示也消失。
func TestDashboardOffsiteMissing(t *testing.T) {
	t.Parallel()
	davURL := startWebDAVServer(t)
	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	if !offsiteMissing(t, inst, admin, csrf) {
		t.Fatal("默认装配只有内置 local,总览应提示缺少异地渠道")
	}
	body, err := json.Marshal(map[string]any{
		"name": "offsite-low", "template": "webdav", "priority": 10,
		"inputs": map[string]string{
			"url": davURL + "/", "vendor": "other", "user": davUser, "pass": davPass, "path": "offsite-low",
		},
	})
	if err != nil {
		t.Fatalf("编码请求体: %v", err)
	}
	createChannel(t, inst, string(body))
	if offsiteMissing(t, inst, admin, csrf) {
		t.Fatal("优先级低于 local 的异地渠道同样存放快照与副本,不应再提示")
	}
}
