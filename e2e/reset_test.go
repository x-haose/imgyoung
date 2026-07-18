//go:build e2e

package e2e

import (
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// resetPasswordRE 从 reset-password 输出中抓新密码(base64url 字符集)。
var resetPasswordRE = regexp.MustCompile(`新密码: ([A-Za-z0-9_-]+)`)

// runResetCLI 在指定实例的数据目录上执行 imgyoung reset-password。
// @param args 追加子命令参数(如 --user)
// @return 命令输出与执行错误
func runResetCLI(t *testing.T, inst *instance, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binPath, append([]string{"reset-password"}, args...)...) //nolint:gosec // binPath 是本测试自行编译的二进制,args 为测试常量
	cmd.Env = append(cmd.Environ(), "IMGYOUNG_DATA_DIR="+inst.dataDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestResetPasswordCLI 锁死链自救闭环:服务运行中执行 CLI 重置密码,
// 新密码可登录、旧密码失效;未知用户报错并列出可用登录名。
func TestResetPasswordCLI(t *testing.T) {
	inst := startInstance(t, nil)

	// 重置前旧密码可登录(基线)。
	if code := postLogin(t, http.DefaultClient, inst.baseURL, "admin", testAdminPass); code != http.StatusOK {
		t.Fatalf("重置前旧密码登录状态码 = %d, 期望 200", code)
	}

	out, err := runResetCLI(t, inst)
	if err != nil {
		t.Fatalf("执行 reset-password: %v\n%s", err, out)
	}
	m := resetPasswordRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("输出未包含新密码:\n%s", out)
	}
	newPassword := m[1]

	if code := postLogin(t, http.DefaultClient, inst.baseURL, "admin", newPassword); code != http.StatusOK {
		t.Fatalf("新密码登录状态码 = %d, 期望 200", code)
	}
	if code := postLogin(t, http.DefaultClient, inst.baseURL, "admin", testAdminPass); code != http.StatusUnauthorized {
		t.Fatalf("旧密码登录状态码 = %d, 期望 401(已失效)", code)
	}

	// 未知用户:非零退出,错误提示列出可用登录名。
	out, err = runResetCLI(t, inst, "--user", "nobody")
	if err == nil {
		t.Fatalf("未知用户应执行失败,输出:\n%s", out)
	}
	if !strings.Contains(out, "admin") {
		t.Fatalf("未知用户的错误提示应列出可用登录名,输出:\n%s", out)
	}
}
