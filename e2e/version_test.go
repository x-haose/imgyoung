//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

// TestVersionFlag -version 打印构建信息后退出,且后台配置接口报告的版本与之一致。
func TestVersionFlag(t *testing.T) {
	t.Parallel()
	if buildErr != nil {
		t.Fatalf("服务二进制不可用: %v", buildErr)
	}
	out, err := exec.Command(binPath, "-version").Output()
	if err != nil {
		t.Fatalf("执行 -version: %v", err)
	}
	line := strings.TrimSpace(string(out))
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "imgyoung" {
		t.Fatalf("-version 输出 = %q, 期望以 imgyoung <版本> 开头", line)
	}

	inst := startInstance(t, nil)
	admin, csrf := adminSession(t, inst)
	resp := adminReq(t, admin, csrf, http.MethodGet, inst.baseURL+"/api/admin/config", "")
	defer func() { _ = resp.Body.Close() }()
	var cfg struct {
		Build struct {
			Version string `json:"version"`
		} `json:"build"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("解析配置响应: %v", err)
	}
	if cfg.Build.Version != fields[1] {
		t.Fatalf("配置接口版本 = %q, 与 -version 输出 %q 不一致", cfg.Build.Version, fields[1])
	}
}
