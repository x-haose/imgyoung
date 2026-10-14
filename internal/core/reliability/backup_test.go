package reliability_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/store/driver"
)

// testEnv 备份测试环境:共用装配之上的备份服务。
type testEnv struct {
	*testenv.Env
	svc *reliability.Service // 被测备份服务,保留 2 份
}

// newTestEnv 建库、写入一条引用记录、装配备份服务(keep=2)。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := testenv.New(t)

	// 造一条可供恢复校验的记录。
	blobID, err := env.Queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "backuphash", Phash: 1, StorageKey: "k_00000000",
		Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob: %v", err)
	}
	if err := env.Queries.CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: "restoreme01", BlobID: blobID, UserID: sqlc.DefaultUserID, FileName: "a.png",
	}); err != nil {
		t.Fatalf("创建引用: %v", err)
	}

	svc := reliability.New(reliability.Config{
		Conn:     env.Conn,
		Channels: env.Manager,
		DataDir:  env.DataDir,
		Keep:     func() int { return 2 },
		Interval: func() time.Duration { return time.Hour },
		Logger:   env.Logger,
	})
	return &testEnv{Env: env, svc: svc}
}

// runStartupOnly 以立即收敛的周期闸调用 Run,只执行其启动阶段。
func runStartupOnly(t *testing.T, env *testEnv) {
	t.Helper()
	env.svc.Run(t.Context(), func(context.Context) bool { return false })
}

// listBackups 列出主渠道上的备份对象键。
func listBackups(t *testing.T, env *testEnv) []driver.ObjectInfo {
	t.Helper()
	primary, err := env.Manager.Primary()
	if err != nil {
		t.Fatalf("取主渠道: %v", err)
	}
	objs, err := primary.Driver.List(t.Context(), reliability.Dir)
	if err != nil {
		t.Fatalf("列举备份目录: %v", err)
	}
	return objs
}

// TestRunOnceUploadsSnapshot 验证备份产出:渠道上出现快照对象,且快照可作为
// 数据库恢复(打开后能查到既有记录)。
func TestRunOnceUploadsSnapshot(t *testing.T) {
	env := newTestEnv(t)
	if err := env.svc.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce 返回错误: %v", err)
	}

	objs := listBackups(t, env)
	if len(objs) != 1 {
		t.Fatalf("备份对象数 = %d, 期望 1", len(objs))
	}

	// 恢复演练:快照(local 渠道即 dataDir/blobs 下的文件)直接作为 DB 打开。
	snapshotPath := filepath.Join(env.DataDir, "blobs", objs[0].Key)
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("快照文件缺失: %v", err)
	}
	restored, err := db.Open(snapshotPath)
	if err != nil {
		t.Fatalf("打开快照数据库: %v", err)
	}
	defer func() {
		if cerr := restored.Close(); cerr != nil {
			t.Errorf("关闭快照数据库: %v", cerr)
		}
	}()
	row, err := sqlc.New(restored).GetImageBlob(t.Context(), "restoreme01")
	if err != nil || row.StorageKey != "k_00000000" {
		t.Fatalf("快照中查询引用失败: %v (%+v)", err, row)
	}
}

// TestRunOncePrunesOldBackups 验证保留策略:超出 keep 份数的旧备份被清理。
func TestRunOncePrunesOldBackups(t *testing.T) {
	env := newTestEnv(t)
	for range 3 {
		if err := env.svc.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce 返回错误: %v", err)
		}
	}
	objs := listBackups(t, env)
	if len(objs) != 2 {
		t.Fatalf("备份对象数 = %d, 期望保留 2 份", len(objs))
	}
}

// TestRunSkipsStartupBackupWhenRecent 验证启动备份只在上一份快照超过间隔时执行:
// 空渠道启动即备份;刚备过再启动不重复;把既有快照改名为一天前的时间戳后再启动则备份。
func TestRunSkipsStartupBackupWhenRecent(t *testing.T) {
	env := newTestEnv(t)
	runStartupOnly(t, env)
	if n := len(listBackups(t, env)); n != 1 {
		t.Fatalf("空渠道首次启动后快照数 = %d, 期望 1", n)
	}
	runStartupOnly(t, env)
	if n := len(listBackups(t, env)); n != 1 {
		t.Fatalf("上一份仍新鲜时再启动快照数 = %d, 期望仍为 1", n)
	}

	// local 渠道即 dataDir/blobs 下的文件:把唯一快照改名成一天前的时间戳。
	backupDir := filepath.Join(env.DataDir, "blobs", reliability.Dir)
	old := filepath.Join(backupDir, "imgyoung-"+time.Now().Add(-24*time.Hour).UTC().Format("20060102-150405.000")+".db")
	if err := os.Rename(filepath.Join(env.DataDir, "blobs", listBackups(t, env)[0].Key), old); err != nil {
		t.Fatalf("改名快照: %v", err)
	}
	runStartupOnly(t, env)
	if n := len(listBackups(t, env)); n != 2 {
		t.Fatalf("上一份已超过间隔时再启动快照数 = %d, 期望 2", n)
	}
}
