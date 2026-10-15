package testenv

import (
	"os"
	"testing"

	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
)

// UseRcloneConfig 让本进程的 rclone 从 path 加载配置文件,测试结束恢复原有的配置路径与配置存储。
// @Description: 改的是 rclone 的进程级全局状态,调用它的用例不得与其他读写 rclone 配置的用例并行。
// @param t 测试上下文,设置失败即 Fatal
// @param path 配置文件路径
func UseRcloneConfig(t *testing.T, path string) {
	t.Helper()
	oldPath, oldData := config.GetConfigPath(), config.Data()
	// 加载配置时 rclone 会改写该环境变量;登记后由测试框架在结束时还原。
	t.Setenv("RCLONE_CONFIG_DIR", os.Getenv("RCLONE_CONFIG_DIR"))
	if err := config.SetConfigPath(path); err != nil {
		t.Fatalf("设置 rclone 配置路径: %v", err)
	}
	configfile.Install()
	t.Cleanup(func() {
		if err := config.SetConfigPath(oldPath); err != nil {
			t.Errorf("还原 rclone 配置路径: %v", err)
		}
		config.SetData(oldData)
	})
}
