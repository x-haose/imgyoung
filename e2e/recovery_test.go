//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestImageRecovery 元数据库丢了一张图的记录、对象还在渠道上:链接 404,找回图片记录后原链接恢复,
// 再找回一次不再登记任何对象。
func TestImageRecovery(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	payload := pngBytes(t)
	imageID, url := mustUpload(t, inst, payload, nil, nil)

	blobID := queryInstanceString(t, inst, "SELECT CAST(blob_id AS TEXT) FROM images WHERE id = $1", imageID)
	execInstanceDB(t, inst, "DELETE FROM blob_channels WHERE blob_id = CAST($1 AS BIGINT)", blobID)
	execInstanceDB(t, inst, "DELETE FROM images WHERE id = $1", imageID)
	execInstanceDB(t, inst, "DELETE FROM blobs WHERE id = CAST($1 AS BIGINT)", blobID)
	gone := fetch(t, inst, url, "")
	goneCode := gone.StatusCode
	_ = gone.Body.Close()
	if goneCode != http.StatusNotFound {
		t.Fatalf("丢记录后的链接状态码 = %d, 期望 404", goneCode)
	}

	client, csrf := adminSession(t, inst)
	for round, wantRecovered := range []int{1, 0} {
		resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/image-recovery", "")
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var res struct {
			Recorded  int `json:"recorded"`
			Recovered int `json:"recovered"`
			Failed    int `json:"failed"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &res)
		}
		if err != nil || resp.StatusCode != http.StatusOK || res.Recovered != wantRecovered || res.Failed != 0 {
			t.Fatalf("第 %d 轮找回: 状态码 %d err=%v body=%s, 期望找回 %d 张且无失败",
				round+1, resp.StatusCode, err, raw, wantRecovered)
		}
	}
	if got := fetchBytes(t, inst, url); !bytes.Equal(got, payload) {
		t.Fatal("找回后原链接不可访问或字节不一致")
	}
}
