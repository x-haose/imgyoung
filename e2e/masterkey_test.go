//go:build e2e

package e2e

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// otherMasterKey 一把与任何实例自动生成的主密钥都不同的合法主密钥(64 位 hex)。
const otherMasterKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// TestMasterKeyGuard 库已用某把主密钥初始化后:主密钥文件不见了、或换成另一把主密钥,进程都拒绝启动
// 并说明原因,不生成新密钥;放回原密钥文件即可照常启动。
func TestMasterKeyGuard(t *testing.T) {
	t.Parallel()
	inst := bootInstance(t)
	listen := strings.TrimPrefix(inst.baseURL, "http://")
	stopInstance(t, inst)

	root, err := os.OpenRoot(inst.dataDir)
	if err != nil {
		t.Fatalf("打开数据目录: %v", err)
	}
	defer func() { _ = root.Close() }()
	const keyFile = "master.key"
	original, err := root.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("读取主密钥文件: %v", err)
	}
	if err := root.Remove(keyFile); err != nil {
		t.Fatalf("移走主密钥文件: %v", err)
	}
	out := runUntilExit(t, inst.dataDir)
	if !strings.Contains(out, "找不到主密钥") {
		t.Fatalf("缺少主密钥时的输出未说明原因:\n%s", out)
	}
	if _, err := root.Stat(keyFile); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("缺少主密钥时不应生成新的密钥文件: %v", err)
	}

	out = runUntilExit(t, inst.dataDir, "IMGYOUNG_MASTER_KEY="+otherMasterKey)
	if !strings.Contains(out, "主密钥与数据库不符") {
		t.Fatalf("主密钥不符时的输出未说明原因:\n%s", out)
	}

	if err := root.WriteFile(keyFile, original, 0o600); err != nil {
		t.Fatalf("放回主密钥文件: %v", err)
	}
	spawn(t, inst, listen)
	waitReady(t, inst)
}

// runUntilExit 在数据目录上启动一次服务进程,等它在 bootDeadline 内自行以非零状态退出。
// @param dataDir 数据目录
// @param env 追加的环境变量(KEY=VALUE)
// @return 进程的全部输出
func runUntilExit(t *testing.T, dataDir string, env ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), bootDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(), "IMGYOUNG_DATA_DIR="+dataDir, "IMGYOUNG_LISTEN=127.0.0.1:0")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("进程未在 %s 内退出:\n%s", bootDeadline, out)
	}
	if err == nil {
		t.Fatalf("进程以零状态退出,期望拒绝启动:\n%s", out)
	}
	return string(out)
}
