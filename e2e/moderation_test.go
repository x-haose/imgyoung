//go:build e2e

package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// backdateImage 把一条引用的上传时刻改到 days 天前,制造冷图。
// @param id 引用短 ID
// @param days 回拨的天数
func backdateImage(t *testing.T, inst *instance, id string, days int) {
	t.Helper()
	old := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(dbTimeLayout)
	if n := execInstanceDB(t, inst, "UPDATE images SET created_at = ? WHERE id = ?", old, id); n != 1 {
		t.Fatalf("回拨上传时刻影响 %d 行, 期望 1", n)
	}
}

// cleanupListed 引用是否在待清理列表中。
// @param id 引用短 ID
// @return 是否在列表中
func cleanupListed(t *testing.T, inst *instance, client *http.Client, csrf, id string) bool {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/cleanup?limit=100", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("待清理列表状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析待清理列表: %v", err)
	}
	for _, it := range lr.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// TestCleanupListAndExempt 冷图扫描把超阈值未访问的引用送进待清理列表;裁决保留走 PATCH 置豁免,
// 豁免后立即离开列表且详情不再标记待清理。
func TestCleanupListAndExempt(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"cold_cleanup_days": 1, "cleanup_sweep_seconds": 1})
	admin, csrf := adminSession(t, inst)
	id, _ := mustUpload(t, inst, pngBytes(t), nil, nil)
	backdateImage(t, inst, id, 3)

	waitUntil(t, 15*time.Second, "冷图进入待清理列表", func() bool {
		return cleanupListed(t, inst, admin, csrf, id)
	})
	code, got := patchImage(t, inst, admin, csrf, id, `{"cleanup_exempt":true}`)
	if code != http.StatusOK || !got.CleanupExempt || got.PendingCleanup {
		t.Fatalf("置豁免: 状态码 %d 响应 %+v, 期望 200、已豁免且不再待清理", code, got)
	}
	if cleanupListed(t, inst, admin, csrf, id) {
		t.Fatal("豁免后仍在待清理列表")
	}
}

// candidateSide 相似候选一方的断言用投影。
type candidateSide struct {
	BlobID  int64  `json:"blob_id"`  // 物理图 id,裁决保留方的标识
	ImageID string `json:"image_id"` // 该物理图任一引用的短 ID
}

// candidateItem 相似候选的断言用投影。
type candidateItem struct {
	ID int64         `json:"id"` // 候选行 id
	A  candidateSide `json:"a"`  // 候选对 A
	B  candidateSide `json:"b"`  // 候选对 B
}

// findCandidate 在待裁决候选里找两张图构成的那一对;两侧顺序不限。
// @param x 一张图的引用短 ID
// @param y 另一张图的引用短 ID
// @return 命中的候选
// @return 是否命中
func findCandidate(t *testing.T, inst *instance, client *http.Client, csrf, x,
	y string) (candidateItem, bool) {
	t.Helper()
	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/dedup?limit=100", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("相似候选列表状态码 = %d, 期望 200", resp.StatusCode)
	}
	var lr struct {
		Items []candidateItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("解析相似候选列表: %v", err)
	}
	for _, c := range lr.Items {
		if (c.A.ImageID == x && c.B.ImageID == y) || (c.A.ImageID == y && c.B.ImageID == x) {
			return c, true
		}
	}
	return candidateItem{}, false
}

// similarJPEGs 生成同一画面的两份 JPEG:编码质量不同故字节与 SHA256 不同,感知哈希相同或极近。
// @Description: variant 选画面(水平渐变或竖向渐变),两对候选用不同画面,互相不混入对方的候选。
// @param variant 画面编号
// @return hq 高质量编码的字节
// @return lq 低质量编码的字节
func similarJPEGs(t *testing.T, variant int) (hq, lq []byte) {
	t.Helper()
	const side = 256
	m := image.NewRGBA(image.Rect(0, 0, side, side))
	var row uint8
	for y := 0; y < side; y++ {
		var col uint8
		for x := 0; x < side; x++ {
			level := col
			if variant%2 == 1 {
				level = row
			}
			m.SetRGBA(x, y, color.RGBA{R: level, G: 255 - level, B: 128, A: 255})
			col++
		}
		row++
	}
	encode := func(quality int) []byte {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: quality}); err != nil {
			t.Fatalf("编码 JPEG: %v", err)
		}
		return buf.Bytes()
	}
	hq, lq = encode(95), encode(60)
	if sha256.Sum256(hq) == sha256.Sum256(lq) {
		t.Fatal("两份 JPEG 字节相同,无法区分精确重复与相似")
	}
	return hq, lq
}

// decideCandidate 对候选执行裁决动作(merge 或 dismiss),返回状态码。
// @param candidateID 候选行 id
// @param action 动作名
// @param body 请求体 JSON,可为空
// @return 状态码
func decideCandidate(t *testing.T, inst *instance, client *http.Client, csrf string, candidateID int64,
	action, body string) int {
	t.Helper()
	target := fmt.Sprintf("%s/api/admin/dedup/%d/%s", inst.baseURL, candidateID, action)
	resp := adminReq(t, client, csrf, http.MethodPost, target, body)
	code := resp.StatusCode
	_ = resp.Body.Close()
	return code
}

// TestDedupCandidatesMergeAndDismiss 相似扫描把字节不同而画面相同的两图送进候选;合并后两引用都仍可访问
// 且候选离开列表;忽略同样让候选离开列表;裁决不存在的候选 404。
func TestDedupCandidatesMergeAndDismiss(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, map[string]any{"dedup_scan_seconds": 1, "dedup_phash_max_distance": 8})
	admin, csrf := adminSession(t, inst)

	first, second := similarJPEGs(t, 0)
	idA, urlA := mustUpload(t, inst, first, nil, nil)
	idB, urlB := mustUpload(t, inst, second, nil, nil)
	var cand candidateItem
	waitUntil(t, 20*time.Second, "相似候选出现", func() bool {
		c, ok := findCandidate(t, inst, admin, csrf, idA, idB)
		cand = c
		return ok
	})
	keep := cand.A.BlobID
	if cand.B.ImageID == idA {
		keep = cand.B.BlobID
	}
	if code := decideCandidate(t, inst, admin, csrf, cand.ID, "merge",
		`{"keep_blob_id":`+strconv.FormatInt(keep, 10)+`}`); code != http.StatusNoContent {
		t.Fatalf("合并状态码 = %d, 期望 204", code)
	}
	for _, u := range []string{urlA, urlB} {
		if got := fetchBytes(t, inst, u); !bytes.Equal(got, first) {
			t.Fatalf("合并后 %s 取回的不是保留方字节", u)
		}
	}
	if _, ok := findCandidate(t, inst, admin, csrf, idA, idB); ok {
		t.Fatal("合并后候选仍在列表")
	}

	third, fourth := similarJPEGs(t, 1)
	idC, _ := mustUpload(t, inst, third, nil, nil)
	idD, _ := mustUpload(t, inst, fourth, nil, nil)
	waitUntil(t, 20*time.Second, "第二对相似候选出现", func() bool {
		c, ok := findCandidate(t, inst, admin, csrf, idC, idD)
		cand = c
		return ok
	})
	if code := decideCandidate(t, inst, admin, csrf, cand.ID, "dismiss", ""); code != http.StatusNoContent {
		t.Fatalf("忽略状态码 = %d, 期望 204", code)
	}
	if _, ok := findCandidate(t, inst, admin, csrf, idC, idD); ok {
		t.Fatal("忽略后候选仍在列表")
	}
	code := decideCandidate(t, inst, admin, csrf, 999999, "merge", `{"keep_blob_id":1}`)
	if code != http.StatusNotFound {
		t.Fatalf("合并不存在候选状态码 = %d, 期望 404", code)
	}
}
