//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestChannelTemplatesDownload 模板清单 API 下发全部内置模板,且不泄露 provider 预设。
func TestChannelTemplatesDownload(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)

	resp := adminReq(t, client, csrf, http.MethodGet, inst.baseURL+"/api/admin/channel-templates", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("模板清单状态码 = %d, 期望 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var lr struct {
		Items []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Driver string `json:"driver"`
			Fields []struct {
				Key     string `json:"key"`
				Type    string `json:"type"`
				Options []struct {
					Label string `json:"label"`
					Value string `json:"value"`
				} `json:"options"`
			} `json:"fields"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("解析模板清单: %v; body=%s", err, raw)
	}
	if lr.Total != int64(len(lr.Items)) || len(lr.Items) < 8 {
		t.Fatalf("模板数 = %d(items %d), 期望 total 与 items 一致且涵盖 S3 系与网盘系",
			lr.Total, len(lr.Items))
	}
	// 预设(provider 等)不应下发到前端。
	if bytes.Contains(raw, []byte("provider")) || bytes.Contains(raw, []byte("endpointPattern")) {
		t.Fatalf("模板清单泄露了内部预设字段: %s", raw)
	}
	// AWS 模板应有 region 下拉且带中英选项。
	var awsRegionOpts int
	for _, it := range lr.Items {
		if it.ID != "aws-s3" {
			continue
		}
		for _, f := range it.Fields {
			if f.Key == "region" {
				if f.Type != "select" {
					t.Errorf("AWS region 字段类型 = %q, 期望 select", f.Type)
				}
				awsRegionOpts = len(f.Options)
			}
		}
	}
	if awsRegionOpts < 5 {
		t.Fatalf("AWS region 下拉选项数 = %d, 期望 ≥5", awsRegionOpts)
	}
}

// TestChannelTemplateValidation 模板模式缺必填字段返回 400 且含字段名;未知模板 400。
func TestChannelTemplateValidation(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	client, csrf := adminSession(t, inst)

	// 缺 region/bucket:应 400。
	missing := `{"name":"aws1","template":"aws-s3","inputs":{"access_key_id":"AK","secret_access_key":"SK"}}`
	resp := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels", missing)
	code := resp.StatusCode
	_ = resp.Body.Close()
	if code != http.StatusBadRequest {
		t.Fatalf("缺必填字段状态码 = %d, 期望 400", code)
	}

	// 未知模板:400。
	unknown := `{"name":"x","template":"no-such","inputs":{}}`
	resp2 := adminReq(t, client, csrf, http.MethodPost, inst.baseURL+"/api/admin/channels", unknown)
	code2 := resp2.StatusCode
	_ = resp2.Body.Close()
	if code2 != http.StatusBadRequest {
		t.Fatalf("未知模板状态码 = %d, 期望 400", code2)
	}
}
