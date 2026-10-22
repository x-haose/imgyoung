//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	neturl "net/url"
	"reflect"
	"strings"
	"testing"
)

// TestImageTags 标签:整体替换、批量追加与移除、按标签筛选、标签清单只含在用的标签;非法标签名与超限返回 400。
func TestImageTags(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)
	idA, _ := mustUpload(t, inst, pngBytes(t), nil, nil)
	idB, _ := mustUpload(t, inst, jpegWithEXIF(t), nil, nil)
	call := func(method, path, body string) (int, []byte) {
		resp := adminReq(t, client, csrf, method, inst.baseURL+path, body)
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, data
	}
	tagsOf := func(id string) []string {
		code, data := call(http.MethodGet, "/api/admin/images/"+id, "")
		var out struct {
			Tags []string `json:"tags"`
		}
		if code != http.StatusOK || json.Unmarshal(data, &out) != nil {
			t.Fatalf("读取图片 %s: %d %s", id, code, data)
		}
		return out.Tags
	}
	filtered := func(tag string) int {
		code, data := call(http.MethodGet, "/api/admin/images?tag="+neturl.QueryEscape(tag), "")
		var out struct {
			Total int `json:"total"`
		}
		if code != http.StatusOK || json.Unmarshal(data, &out) != nil {
			t.Fatalf("按标签筛选: %d %s", code, data)
		}
		return out.Total
	}

	replace := `{"tags":["blog"," 旅行 ","blog"]}`
	if code, data := call(http.MethodPatch, "/api/admin/images/"+idA, replace); code != http.StatusOK {
		t.Fatalf("整体替换标签: %d %s", code, data)
	}
	if got := tagsOf(idA); !reflect.DeepEqual(got, []string{"blog", "旅行"}) {
		t.Fatalf("图 A 的标签 = %v", got)
	}
	body := `{"ids":["` + idA + `","` + idB + `","nope"],"add":["draft"],"remove":["blog"]}`
	code, data := call(http.MethodPost, "/api/admin/images/batch-tags", body)
	var batch struct {
		Updated int `json:"updated"`
		Failed  []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"failed"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &batch) != nil || batch.Updated != 2 ||
		len(batch.Failed) != 1 || batch.Failed[0].ID != "nope" || batch.Failed[0].Code != "image_not_found" {
		t.Fatalf("批量改标签: %d %s", code, data)
	}
	if got := tagsOf(idA); !reflect.DeepEqual(got, []string{"draft", "旅行"}) {
		t.Fatalf("批量之后图 A 的标签 = %v", got)
	}
	if n := filtered("draft"); n != 2 {
		t.Errorf("按 draft 筛选 = %d, 期望 2", n)
	}
	if n := filtered("旅行"); n != 1 {
		t.Errorf("按 旅行 筛选 = %d, 期望 1", n)
	}
	if n := filtered("blog"); n != 0 {
		t.Errorf("按已移除的 blog 筛选 = %d, 期望 0", n)
	}
	if code, data := call(http.MethodGet, "/api/admin/tags", ""); code != http.StatusOK ||
		!strings.Contains(string(data), `"draft"`) || strings.Contains(string(data), `"blog"`) {
		t.Errorf("标签清单 = %d %s, 期望含 draft、不含已无图片使用的 blog", code, data)
	}

	for name, patch := range map[string]string{
		"空名":   `{"tags":["  "]}`,
		"过长":   `{"tags":["` + strings.Repeat("x", 33) + `"]}`,
		"含控制符": `{"tags":["a\u0000b"]}`,
		"超过上限": `{"tags":["` + strings.Join(manyTags(21), `","`) + `"]}`,
	} {
		if code, _ := call(http.MethodPatch, "/api/admin/images/"+idA, patch); code != http.StatusBadRequest {
			t.Errorf("%s: 状态码 = %d, 期望 400", name, code)
		}
	}
	if got := tagsOf(idA); !reflect.DeepEqual(got, []string{"draft", "旅行"}) {
		t.Errorf("被拒的修改改动了标签: %v", got)
	}
	if code, _ := call(http.MethodPatch, "/api/admin/images/"+idA, `{"tags":[]}`); code != http.StatusOK ||
		len(tagsOf(idA)) != 0 {
		t.Error("空数组应清除全部标签")
	}
}

// manyTags 生成 n 个不同的标签名。
func manyTags(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "t" + strings.Repeat("a", i)
	}
	return out
}
