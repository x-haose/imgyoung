//go:build e2e

// 批量导入的 e2e 覆盖:创建、扫描、确认、重试、取消、删除与对照表的接口行为,以及任务只对创建者可见。
// 真实抓取器按设计拒绝环回与内网地址,所以这里导入的条目都以 url_blocked 失败;抓取成功的路径
// 由 internal/core/importer 的集成测试以抓取器替身覆盖。
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// importView 导入任务详情中本文件断言用到的字段。
type importView struct {
	ID       string `json:"id"`        // 任务 ID
	UserID   string `json:"user_id"`   // 创建任务的用户
	UserName string `json:"user_name"` // 创建任务的用户的登录名
	Mode     string `json:"mode"`      // 输入类型
	Status   string `json:"status"`    // 任务状态
	Counts   struct {
		Candidate int64 `json:"candidate"` // 待确认
		Pending   int64 `json:"pending"`   // 待转存
		Done      int64 `json:"done"`      // 已导入
		Failed    int64 `json:"failed"`    // 失败
		Skipped   int64 `json:"skipped"`   // 已跳过
	} `json:"counts"` // 各状态的条目数
	Scan struct {
		PagesFound int64 `json:"pages_found"` // 已发现的页面数
		Report     *struct {
			FailedPages []struct {
				Source string `json:"source"` // 页面地址
				Code   string `json:"code"`   // 原因码
			} `json:"failed_pages"` // 没有抓取成功的页面
		} `json:"report"` // 扫描报告
	} `json:"scan"` // 扫描进度与结果
}

// importAPI 绑定一个会话的导入接口调用器。
type importAPI struct {
	t      *testing.T
	inst   *instance
	client *http.Client // 带会话 cookie 的客户端
	csrf   string       // 该会话的 CSRF 令牌
}

// call 发送导入接口请求,返回状态码与响应体。
func (a importAPI) call(method, path, body string) (int, []byte) {
	a.t.Helper()
	resp := adminReq(a.t, a.client, a.csrf, method, a.inst.baseURL+"/api/admin/imports"+path, body)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatalf("读响应: %v", err)
	}
	return resp.StatusCode, raw
}

// errorCodeOf 取错误信封里的 code。
func errorCodeOf(t *testing.T, raw []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("解析错误信封: %v; body=%s", err, raw)
	}
	return e.Code
}

// wantImportError 断言一次调用返回指定状态码与错误码。
func wantImportError(t *testing.T, what string, status int, raw []byte, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus || errorCodeOf(t, raw) != wantCode {
		t.Fatalf("%s: 状态码 = %d 错误码 = %q, 期望 %d %q; body=%s",
			what, status, errorCodeOf(t, raw), wantStatus, wantCode, raw)
	}
}

// decodeImport 解析任务详情。
func decodeImport(t *testing.T, raw []byte) importView {
	t.Helper()
	var v importView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("解析任务详情: %v; body=%s", err, raw)
	}
	return v
}

// create 创建任务并断言 201。
func (a importAPI) create(body string) importView {
	a.t.Helper()
	status, raw := a.call(http.MethodPost, "", body)
	if status != http.StatusCreated {
		a.t.Fatalf("创建任务状态码 = %d, 期望 201; body=%s", status, raw)
	}
	return decodeImport(a.t, raw)
}

// wait 轮询任务直到进入指定状态。
func (a importAPI) wait(id, want string) importView {
	a.t.Helper()
	var last importView
	waitUntil(a.t, 20*time.Second, "导入任务进入 "+want, func() bool {
		status, raw := a.call(http.MethodGet, "/"+id, "")
		if status != http.StatusOK {
			a.t.Fatalf("读任务状态码 = %d; body=%s", status, raw)
		}
		last = decodeImport(a.t, raw)
		return last.Status == want
	})
	return last
}

// TestImportImagesFlow 图片地址清单:被地址策略拦下的条目逐条失败、任务仍完成;可重试、不能取消已结束的任务;
// 对照表为空;删除后任务不存在。非法输入逐项被拒绝。
func TestImportImagesFlow(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	api := importAPI{t: t, inst: inst, client: client, csrf: csrf}

	job := api.create(`{"mode":"images","urls":["http://127.0.0.1:1/a.png","http://10.0.0.1/b.png"],` +
		`"ttl":"never","exif_policy":"strip"}`)
	if job.Mode != "images" {
		t.Fatalf("mode = %q", job.Mode)
	}
	done := api.wait(job.ID, "done")
	if done.Counts.Failed != 2 || done.Counts.Done != 0 {
		t.Fatalf("各状态 = %+v, 期望 failed=2", done.Counts)
	}

	status, raw := api.call(http.MethodGet, "/"+job.ID+"/items?state=failed", "")
	var items struct {
		Items []struct {
			ErrorCode string `json:"error_code"` // 失败原因码
		} `json:"items"` // 条目
		Total int64 `json:"total"` // 总数
	}
	if err := json.Unmarshal(raw, &items); err != nil || status != http.StatusOK || items.Total != 2 {
		t.Fatalf("失败条目 = %d %s", status, raw)
	}
	for _, it := range items.Items {
		if it.ErrorCode != "url_blocked" {
			t.Fatalf("失败原因 = %q, 期望 url_blocked", it.ErrorCode)
		}
	}

	if status, raw = api.call(http.MethodGet, "/"+job.ID+"/mapping", ""); status != http.StatusOK ||
		strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("JSON 对照表 = %d %s, 期望 []", status, raw)
	}
	if status, raw = api.call(http.MethodGet, "/"+job.ID+"/mapping?format=csv", ""); status != http.StatusOK ||
		strings.TrimSpace(string(raw)) != "old_url,new_url,image_id,source,raw" {
		t.Fatalf("CSV 对照表 = %d %s, 期望只有表头", status, raw)
	}

	if status, raw = api.call(http.MethodPost, "/"+job.ID+"/retry", ""); status != http.StatusOK {
		t.Fatalf("重试状态码 = %d; body=%s", status, raw)
	}
	api.wait(job.ID, "done")
	status, raw = api.call(http.MethodPost, "/"+job.ID+"/cancel", "")
	wantImportError(t, "取消已结束的任务", status, raw, http.StatusConflict, "import_state_invalid")

	if status, raw = api.call(http.MethodDelete, "/"+job.ID, ""); status != http.StatusNoContent {
		t.Fatalf("删除状态码 = %d; body=%s", status, raw)
	}
	status, raw = api.call(http.MethodGet, "/"+job.ID, "")
	wantImportError(t, "读已删除的任务", status, raw, http.StatusNotFound, "import_not_found")
}

// TestImportRejectsInvalidInput 创建请求的各种非法输入都以 400 拒绝,错误码可识别。
func TestImportRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	api := importAPI{t: t, inst: inst, client: client, csrf: csrf}

	cases := []struct {
		name, body, code string
	}{
		{"不是 JSON", `not json`, "invalid_body"},
		{"未知字段", `{"mode":"images","urls":["https://a.example.com/a.png"],"x":1}`, "invalid_body"},
		{"缺少 mode", `{"urls":["https://a.example.com/a.png"]}`, "import_input_invalid"},
		{"mode 非法", `{"mode":"nope"}`, "import_input_invalid"},
		{"images 缺地址", `{"mode":"images","urls":[]}`, "import_input_invalid"},
		{"images 地址不合法", `{"mode":"images","urls":["not a url"]}`, "import_input_invalid"},
		{"images 带了别的模式的字段",
			`{"mode":"images","urls":["https://a.example.com/a.png"],"sites":["https://a.example.com/"]}`,
			"import_input_invalid"},
		{"text 缺文档", `{"mode":"text","documents":[]}`, "import_input_invalid"},
		{"text 基准地址不合法", `{"mode":"text","documents":[{"name":"a.md","text":"x"}],"base_url":"blog"}`,
			"import_input_invalid"},
		{"site 地址不合法", `{"mode":"site","sites":["ftp://a.example.com/sitemap.xml"]}`, "import_input_invalid"},
		{"ttl 非法", `{"mode":"images","urls":["https://a.example.com/a.png"],"ttl":"abc"}`,
			"import_input_invalid"},
		{"exif_policy 非法", `{"mode":"images","urls":["https://a.example.com/a.png"],"exif_policy":"x"}`,
			"import_input_invalid"},
		{"referer 非法", `{"mode":"images","urls":["https://a.example.com/a.png"],"referer":"blog"}`,
			"import_input_invalid"},
	}
	for _, tc := range cases {
		status, raw := api.call(http.MethodPost, "", tc.body)
		wantImportError(t, tc.name, status, raw, http.StatusBadRequest, tc.code)
	}
}

// TestImportTextConfirmFlow 文本扫描后任务等待确认:按域名分组;所选域名下没有条目被拒绝;确认后只处理所选域名,
// 其余记为跳过;已确认的任务不能再确认。
func TestImportTextConfirmFlow(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	api := importAPI{t: t, inst: inst, client: client, csrf: csrf}

	job := api.create(`{"mode":"text","documents":[{"name":"a.md","text":` +
		`"![x](http://127.0.0.1:1/x.png)\n\n![y](https://other.invalid/y.png)"}]}`)
	if job.Status != "ready" || job.Counts.Candidate != 2 {
		t.Fatalf("任务 = %+v, 期望 ready 且有 2 个候选", job)
	}
	status, raw := api.call(http.MethodGet, "/"+job.ID+"/hosts", "")
	var hosts []struct {
		Host  string `json:"host"`  // 域名
		Count int64  `json:"count"` // 条目数
	}
	if err := json.Unmarshal(raw, &hosts); err != nil || status != http.StatusOK || len(hosts) != 2 {
		t.Fatalf("域名分组 = %d %s", status, raw)
	}

	status, raw = api.call(http.MethodPost, "/"+job.ID+"/start", `{"hosts":[]}`)
	wantImportError(t, "没选域名", status, raw, http.StatusBadRequest, "invalid_body")
	status, raw = api.call(http.MethodPost, "/"+job.ID+"/start", `{"hosts":["nope.example.com"]}`)
	wantImportError(t, "所选域名下没有条目", status, raw, http.StatusBadRequest, "import_no_selection")
	if status, raw = api.call(http.MethodPost, "/"+job.ID+"/start",
		`{"hosts":["127.0.0.1:1"]}`); status != http.StatusOK {
		t.Fatalf("确认状态码 = %d; body=%s", status, raw)
	}

	done := api.wait(job.ID, "done")
	if done.Counts.Failed != 1 || done.Counts.Skipped != 1 || done.Counts.Candidate != 0 {
		t.Fatalf("各状态 = %+v, 期望 failed=1 skipped=1", done.Counts)
	}
	status, raw = api.call(http.MethodPost, "/"+job.ID+"/start", `{"hosts":["127.0.0.1:1"]}`)
	wantImportError(t, "重复确认", status, raw, http.StatusConflict, "import_state_invalid")
}

// TestImportSiteScan 站点任务:服务端抓取被地址策略拦下,记入扫描报告;没有候选时不能开始;待确认的任务可以删除。
func TestImportSiteScan(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	api := importAPI{t: t, inst: inst, client: client, csrf: csrf}

	job := api.create(`{"mode":"site","sites":["http://127.0.0.1:1/sitemap.xml"]}`)
	if job.Mode != "site" {
		t.Fatalf("mode = %q", job.Mode)
	}
	ready := api.wait(job.ID, "ready")
	if ready.Scan.Report == nil || len(ready.Scan.Report.FailedPages) != 1 ||
		ready.Scan.Report.FailedPages[0].Code != "url_blocked" {
		t.Fatalf("扫描报告 = %+v, 期望记下 url_blocked", ready.Scan.Report)
	}
	status, raw := api.call(http.MethodPost, "/"+job.ID+"/start", `{"hosts":["127.0.0.1:1"]}`)
	wantImportError(t, "没有候选时开始", status, raw, http.StatusBadRequest, "import_no_selection")
	if status, raw = api.call(http.MethodDelete, "/"+job.ID, ""); status != http.StatusNoContent {
		t.Fatalf("删除待确认的任务状态码 = %d; body=%s", status, raw)
	}
}

// TestImportOwnership 普通用户只能看到和操作自己的任务,别人的一律按不存在处理;管理员能看到全部、取消与删除任何人的,
// 但确认与重试只有创建者能做。
func TestImportOwnership(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	admin, adminCSRF := adminSession(t, inst)
	createUser(t, inst, admin, adminCSRF, "bob", "bob-secret-1", 0)
	bob, bobCSRF := loginSession(t, inst, "bob", "bob-secret-1")
	adminAPI := importAPI{t: t, inst: inst, client: admin, csrf: adminCSRF}
	bobAPI := importAPI{t: t, inst: inst, client: bob, csrf: bobCSRF}

	body := `{"mode":"text","documents":[{"name":"a.md","text":"![x](https://a.example.com/x.png)"}]}`
	adminJob := adminAPI.create(body)
	bobJob := bobAPI.create(body)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/" + adminJob.ID}, {http.MethodGet, "/" + adminJob.ID + "/hosts"},
		{http.MethodGet, "/" + adminJob.ID + "/items"}, {http.MethodGet, "/" + adminJob.ID + "/mapping"},
		{http.MethodPost, "/" + adminJob.ID + "/start"}, {http.MethodPost, "/" + adminJob.ID + "/cancel"},
		{http.MethodPost, "/" + adminJob.ID + "/retry"}, {http.MethodDelete, "/" + adminJob.ID},
	} {
		reqBody := ""
		if strings.HasSuffix(c.path, "/start") {
			reqBody = `{"hosts":["a.example.com"]}`
		}
		status, raw := bobAPI.call(c.method, c.path, reqBody)
		wantImportError(t, c.method+" "+c.path, status, raw, http.StatusNotFound, "import_not_found")
	}
	if total := listTotal(t, bobAPI, ""); total != 1 {
		t.Fatalf("bob 的任务列表总数 = %d, 期望只有自己的 1 个", total)
	}
	if total := listTotal(t, adminAPI, ""); total != 2 {
		t.Fatalf("管理员的任务列表总数 = %d, 期望看到全部 2 个", total)
	}
	if total := listTotal(t, adminAPI, "?user_id="+bobJob.UserID); total != 1 {
		t.Fatalf("按 user_id 过滤后的总数 = %d, 期望 1", total)
	}
	if got := adminAPI.get(bobJob.ID); got.UserName != "bob" || got.UserID == "" {
		t.Fatalf("管理员读别人的任务 = %+v, 期望带创建者信息", got)
	}

	status, raw := adminAPI.call(http.MethodPost, "/"+bobJob.ID+"/start", `{"hosts":["a.example.com"]}`)
	wantImportError(t, "管理员确认别人的任务", status, raw, http.StatusForbidden, "import_owner_only")
	status, raw = adminAPI.call(http.MethodPost, "/"+bobJob.ID+"/retry", "")
	wantImportError(t, "管理员重试别人的任务", status, raw, http.StatusForbidden, "import_owner_only")
	if status, raw = adminAPI.call(http.MethodPost, "/"+bobJob.ID+"/cancel", ""); status != http.StatusOK {
		t.Fatalf("管理员取消别人的任务状态码 = %d; body=%s", status, raw)
	}
	if status, raw = adminAPI.call(http.MethodDelete, "/"+bobJob.ID, ""); status != http.StatusNoContent {
		t.Fatalf("管理员删除别人的任务状态码 = %d; body=%s", status, raw)
	}
	if got := adminAPI.wait(adminJob.ID, "ready"); got.Counts.Candidate != 1 {
		t.Fatalf("别人的操作不应影响任务: %+v", got)
	}
}

// get 读取任务详情并断言 200。
func (a importAPI) get(id string) importView {
	a.t.Helper()
	status, raw := a.call(http.MethodGet, "/"+id, "")
	if status != http.StatusOK {
		a.t.Fatalf("读任务状态码 = %d; body=%s", status, raw)
	}
	return decodeImport(a.t, raw)
}

// listTotal 列出任务并返回总数。
func listTotal(t *testing.T, api importAPI, query string) int64 {
	t.Helper()
	status, raw := api.call(http.MethodGet, query, "")
	var list struct {
		Total int64 `json:"total"` // 总数
	}
	if err := json.Unmarshal(raw, &list); err != nil || status != http.StatusOK {
		t.Fatalf("任务列表 = %d %s", status, raw)
	}
	return list.Total
}
