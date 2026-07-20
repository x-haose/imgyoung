package reliability_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
	"github.com/x-haose/imgyoung/internal/store/driver"
)

// testEnv 备份测试环境。
type testEnv struct {
	svc     *reliability.Service
	queries *sqlc.Queries
	mgr     *channel.Manager
	dataDir string
}

// newTestEnv 建库、写入一条引用记录、装配备份服务(keep=2)。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "imgyoung.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() {
		if cerr := conn.Close(); cerr != nil {
			t.Errorf("关闭测试数据库: %v", cerr)
		}
	})
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	queries := sqlc.New(conn)
	mgr, err := channel.NewManager(t.Context(), channel.ManagerConfig{
		Conn: conn, Queries: queries, Master: master, DataDir: dir,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("创建渠道管理器: %v", err)
	}

	// 造一条可供恢复校验的记录。
	blobID, err := queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "backuphash", Phash: 1, StorageKey: "k_00000000",
		Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob: %v", err)
	}
	if err := queries.CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: "restoreme01", BlobID: blobID, UserID: sqlc.DefaultUserID, FileName: "a.png",
	}); err != nil {
		t.Fatalf("创建引用: %v", err)
	}

	svc := reliability.New(reliability.Config{
		Conn:     conn,
		Channels: mgr,
		DataDir:  dir,
		Keep:     func() int { return 2 },
		Logger:   slog.New(slog.DiscardHandler),
	})
	return &testEnv{svc: svc, queries: queries, mgr: mgr, dataDir: dir}
}

// listBackups 列出主渠道上的备份对象键。
func listBackups(t *testing.T, env *testEnv) []driver.ObjectInfo {
	t.Helper()
	primary, err := env.mgr.Primary()
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
	snapshotPath := filepath.Join(env.dataDir, "blobs", objs[0].Key)
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
