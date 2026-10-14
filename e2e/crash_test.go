//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

// taggedPNG 生成一张像素值随 tag 变化的 PNG,多张互不相同,不触发精确去重。
// @param tag 区分值
// @return PNG 字节
func taggedPNG(t *testing.T, tag uint8) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 4, 4))
	level := tag
	for i := range m.Pix {
		m.Pix[i] = level
		level += 7
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatalf("编码 PNG: %v", err)
	}
	return buf.Bytes()
}

// replicatedAll 渠道目录下是否已有每个引用对应的对象文件(对象键以引用短 ID 开头)。
// @param dir 渠道存储目录
// @param ids 引用短 ID 列表
// @return 是否全部到齐
func replicatedAll(dir string, ids []string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, id := range ids {
		found := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), id+"_") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestCrashRecoveryReplicates 进程被硬杀(SIGKILL)后在同一数据目录重启:已上传的图全部可访问,
// 崩溃前尚未补齐到备份渠道的副本由启动对账重新入队并追平。
func TestCrashRecoveryReplicates(t *testing.T) {
	t.Parallel()
	inst := startInstance(t, nil)
	backupDir := t.TempDir()
	createChannel(t, inst, fmt.Sprintf(
		`{"name":"backup","driver":"local","priority":10,"config":{"path":%q}}`, backupDir))

	const n = 5
	ids := make([]string, 0, n)
	urls := make([]string, 0, n)
	payloads := make([][]byte, 0, n)
	for tag := uint8(1); tag <= n; tag++ {
		payload := taggedPNG(t, tag)
		id, u := mustUpload(t, inst, payload, nil, nil)
		ids = append(ids, id)
		urls = append(urls, u)
		payloads = append(payloads, payload)
	}

	// 硬杀:不给任何收尾机会,元数据库与任务队列只能靠落盘状态恢复。
	if err := inst.cmd.Process.Kill(); err != nil {
		t.Fatalf("硬杀进程: %v", err)
	}
	_ = inst.cmd.Wait()
	inst2 := startInstanceAt(t, inst, strings.TrimPrefix(inst.baseURL, "http://"))

	for i, u := range urls {
		if got := fetchBytes(t, inst2, u); !bytes.Equal(got, payloads[i]) {
			t.Fatalf("重启后第 %d 张图字节不一致", i+1)
		}
	}
	waitUntil(t, 30*time.Second, "崩溃前未补齐的副本追平到备份渠道", func() bool {
		return replicatedAll(backupDir, ids)
	})
}
