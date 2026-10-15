package main

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// clearBootEnv 移除 IMGYOUNG_ 环境变量(测试结束恢复),避免开发机残留影响装配;
// 设为空串是显式置空,与未设置语义不同,只能移除。
func clearBootEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "IMGYOUNG_") {
			t.Setenv(k, "") // 登记测试结束时恢复原值
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("移除环境变量 %s: %v", k, err)
			}
		}
	}
}

// TestBuildAssembles 验证手工装配整链构造成功、cleanup 干净关闭。
// @Description: 守卫装配根接线回归(nil 依赖、构造 panic、迁移失败、宽表默认行装载等);
// serve() 的信号停机顺序不在此覆盖(由预览实例 SIGTERM 实测),避免引入 flaky 计时断言。
func TestBuildAssembles(t *testing.T) {
	clearBootEnv(t)
	t.Setenv("IMGYOUNG_DATA_DIR", t.TempDir())

	a, cleanup, err := build(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("build 返回错误: %v", err)
	}
	if a == nil || a.httpSrv == nil || a.store == nil || a.runner == nil {
		t.Fatalf("装配结果不完整: %+v", a)
	}
	if cleanup == nil {
		t.Fatal("cleanup 为 nil")
	}
	// 出厂默认经宽表装载可用。
	if a.store.Current().MaxUploadMB != 50 {
		t.Fatalf("默认配置未装载: max_upload_mb = %d", a.store.Current().MaxUploadMB)
	}
	cleanup() // 关数据库,应无 panic
}
