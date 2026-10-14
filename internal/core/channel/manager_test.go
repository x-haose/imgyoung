package channel_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// testEnv 管理器测试环境。
type testEnv struct {
	mgr     *channel.Manager
	queries *sqlc.Queries
	dataDir string
}

// newTestEnv 建库迁移(迁移自带默认 local 渠道)并创建管理器。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	conn, queries := dbtest.New(t)
	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	mgr, err := channel.NewManager(t.Context(), channel.ManagerConfig{
		Conn:    conn,
		Queries: queries,
		Master:  master,
		DataDir: dir,
		Logger:  slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewManager 返回错误: %v", err)
	}
	return &testEnv{mgr: mgr, queries: queries, dataDir: dir}
}

// addChannel 创建一条渠道记录(local 后端指向独立临时目录)并重载管理器。
// @return 渠道 ID 与其存储目录
func addChannel(t *testing.T, env *testEnv, name string, priority int64, encrypted bool) (int64, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := json.Marshal(map[string]string{"path": dir})
	if err != nil {
		t.Fatalf("编码渠道配置: %v", err)
	}
	sealed, err := env.mgr.SealConfig(string(cfg))
	if err != nil {
		t.Fatalf("加密渠道配置: %v", err)
	}
	enc := int64(0)
	if encrypted {
		enc = 1
	}
	id, err := env.queries.CreateChannel(t.Context(), sqlc.CreateChannelParams{
		Name:         name,
		Driver:       "local",
		ConfigSealed: sealed,
		Priority:     priority,
		Encrypted:    enc,
	})
	if err != nil {
		t.Fatalf("创建渠道记录: %v", err)
	}
	if err := env.mgr.Reload(t.Context()); err != nil {
		t.Fatalf("重载管理器: %v", err)
	}
	return id, dir
}

// TestManagerLoadsBuiltinLocal 验证迁移内置的默认 local 渠道被加载为主渠道。
func TestManagerLoadsBuiltinLocal(t *testing.T) {
	env := newTestEnv(t)
	primary, err := env.mgr.Primary()
	if err != nil {
		t.Fatalf("Primary 返回错误: %v", err)
	}
	if primary.Name != "local" {
		t.Fatalf("主渠道 = %q, 期望内置 local", primary.Name)
	}
	// 默认 local 的存储路径应落在 dataDir/blobs 下。
	payload := []byte("builtin-local-object")
	if err := primary.Driver.Put(t.Context(), "probe.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("写入默认渠道: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.dataDir, "blobs", "probe.bin")); err != nil {
		t.Fatalf("对象未落在 dataDir/blobs: %v", err)
	}
}

// TestManagerPrimaryByPriority 验证主渠道 = 健康渠道中优先级最高者。
func TestManagerPrimaryByPriority(t *testing.T) {
	env := newTestEnv(t)
	id, _ := addChannel(t, env, "backup-high", 200, false)

	primary, err := env.mgr.Primary()
	if err != nil {
		t.Fatalf("Primary 返回错误: %v", err)
	}
	if primary.ID != id {
		t.Fatalf("主渠道 ID = %d, 期望优先级更高的 %d", primary.ID, id)
	}
	if got := env.mgr.All(); len(got) != 2 || got[0].ID != id {
		t.Fatalf("All() 顺序异常: %+v", got)
	}
}

// TestManagerSkipsUnhealthyPrimary 验证不健康渠道不参与主渠道选择。
func TestManagerSkipsUnhealthyPrimary(t *testing.T) {
	env := newTestEnv(t)
	id, _ := addChannel(t, env, "backup-high", 200, false)
	if err := env.queries.UpdateChannelHealth(t.Context(), sqlc.UpdateChannelHealthParams{Healthy: 0, ID: id}); err != nil {
		t.Fatalf("标记渠道不健康: %v", err)
	}
	if err := env.mgr.Reload(t.Context()); err != nil {
		t.Fatalf("重载管理器: %v", err)
	}
	primary, err := env.mgr.Primary()
	if err != nil {
		t.Fatalf("Primary 返回错误: %v", err)
	}
	if primary.Name != "local" {
		t.Fatalf("主渠道 = %q, 期望降级回 local", primary.Name)
	}
}

// TestManagerEncryptedChannel 验证加密渠道:落盘为密文与加密文件名,经驱动读回为明文。
func TestManagerEncryptedChannel(t *testing.T) {
	env := newTestEnv(t)
	_, dir := addChannel(t, env, "vault", 200, true)

	primary, err := env.mgr.Primary()
	if err != nil {
		t.Fatalf("Primary 返回错误: %v", err)
	}
	plain := []byte("plaintext-image-bytes-0123456789")
	if err := primary.Driver.Put(t.Context(), "img.bin", bytes.NewReader(plain), int64(len(plain))); err != nil {
		t.Fatalf("写入加密渠道: %v", err)
	}

	// 磁盘上:文件名被加密(找不到 img.bin),内容不含明文。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读渠道目录: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("加密渠道目录为空,写入未落盘")
	}
	for _, e := range entries {
		if e.Name() == "img.bin" {
			t.Fatal("文件名未被加密")
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("读密文文件: %v", rerr)
		}
		if bytes.Contains(raw, plain) {
			t.Fatal("磁盘文件含明文内容")
		}
	}

	// 经驱动读回:明文一致。
	rc, err := primary.Driver.Open(t.Context(), "img.bin")
	if err != nil {
		t.Fatalf("读回对象: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("关闭对象流: %v", cerr)
	}
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("读回明文不一致: err=%v", err)
	}
}

// TestManagerCandidatesForBlob 验证按 blob 的候选渠道:仅健康且副本完成者,按优先级排序。
func TestManagerCandidatesForBlob(t *testing.T) {
	env := newTestEnv(t)
	highID, _ := addChannel(t, env, "backup-high", 300, false)

	// 造一个 blob:副本只在内置 local(done),高优先级渠道为 pending。
	blobID, err := env.queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "ab", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob: %v", err)
	}
	local, err := env.mgr.Primary() // 此时主渠道是 backup-high
	if err != nil {
		t.Fatalf("Primary: %v", err)
	}
	if local.ID == highID {
		// 取内置 local 的句柄
		for _, h := range env.mgr.All() {
			if h.ID != highID {
				local = h
			}
		}
	}
	mustUpsert := func(chID int64, state string) {
		t.Helper()
		if err := env.queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: chID, State: state,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	mustUpsert(local.ID, "done")
	mustUpsert(highID, "pending")

	got, err := env.mgr.CandidatesForBlob(t.Context(), blobID)
	if err != nil {
		t.Fatalf("CandidatesForBlob 返回错误: %v", err)
	}
	if len(got) != 1 || got[0].ID != local.ID {
		t.Fatalf("候选 = %+v, 期望仅内置 local(唯一 done 副本)", got)
	}

	// 高优先级副本完成后,应排在候选首位。
	mustUpsert(highID, "done")
	got, err = env.mgr.CandidatesForBlob(t.Context(), blobID)
	if err != nil {
		t.Fatalf("CandidatesForBlob 返回错误: %v", err)
	}
	if len(got) != 2 || got[0].ID != highID {
		t.Fatalf("候选顺序 = %+v, 期望高优先级在前", got)
	}
}

// TestManagerGet 验证按 ID 取句柄。
func TestManagerGet(t *testing.T) {
	env := newTestEnv(t)
	id, _ := addChannel(t, env, "backup-high", 200, false)
	if h, ok := env.mgr.Get(id); !ok || h.Name != "backup-high" {
		t.Fatalf("Get(%d) = %+v/%v, 期望命中 backup-high", id, h, ok)
	}
	if _, ok := env.mgr.Get(99999); ok {
		t.Fatal("Get(不存在的 ID) 应返回 false")
	}
}

// TestManagerCheckAll 验证健康探测:渠道故障被感知、恢复后回归。
func TestManagerCheckAll(t *testing.T) {
	env := newTestEnv(t)
	id, dir := addChannel(t, env, "flaky", 300, false)

	changed, err := env.mgr.CheckAll(t.Context())
	if err != nil || len(changed) != 0 {
		t.Fatalf("初始探测 = %v/%v, 期望无变化", changed, err)
	}

	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("模拟渠道故障: %v", err)
	}
	defer func() {
		if cerr := os.Chmod(dir, 0o700); cerr != nil { //nolint:gosec // 目录需执行位方可进入,0700 已是最小权限
			t.Errorf("恢复目录权限: %v", cerr)
		}
	}()
	changed, err = env.mgr.CheckAll(t.Context())
	if err != nil || len(changed) != 1 || changed[0] != "flaky" {
		t.Fatalf("故障探测 = %v/%v, 期望 flaky 状态翻转", changed, err)
	}
	primary, err := env.mgr.Primary()
	if err != nil || primary.Name != "local" {
		t.Fatalf("故障后主渠道 = %v/%v, 期望降级回 local", primary.Name, err)
	}

	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 目录需执行位方可进入,0700 已是最小权限
		t.Fatalf("恢复渠道: %v", err)
	}
	changed, err = env.mgr.CheckAll(t.Context())
	if err != nil || len(changed) != 1 {
		t.Fatalf("恢复探测 = %v/%v, 期望 flaky 回归", changed, err)
	}
	if primary, _ := env.mgr.Primary(); primary.Name != "flaky" {
		t.Fatalf("恢复后主渠道 = %q, 期望 flaky", primary.Name)
	}
	if _, ok := env.mgr.Get(id); !ok {
		t.Fatal("恢复后渠道句柄丢失")
	}
}

// TestManagerNoHealthyChannel 验证全部不健康时 Primary 返回明确错误。
func TestManagerNoHealthyChannel(t *testing.T) {
	env := newTestEnv(t)
	for _, h := range env.mgr.All() {
		if err := env.queries.UpdateChannelHealth(t.Context(), sqlc.UpdateChannelHealthParams{Healthy: 0, ID: h.ID}); err != nil {
			t.Fatalf("标记不健康: %v", err)
		}
	}
	if err := env.mgr.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if _, err := env.mgr.Primary(); !errors.Is(err, channel.ErrNoHealthyChannel) {
		t.Fatalf("错误 = %v, 期望 ErrNoHealthyChannel", err)
	}
}
