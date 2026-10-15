package channel_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// firstHealthy 返回健康且不在清理中的渠道里优先级最高者(上传首选的渠道);一个都没有即失败。
func firstHealthy(t *testing.T, m *channel.Manager) channel.Handle {
	t.Helper()
	healthy := m.Healthy()
	if len(healthy) == 0 {
		t.Fatal("没有健康渠道")
	}
	return healthy[0]
}

// TestManagerLoadsBuiltinLocal 验证迁移内置的默认 local 渠道被加载为唯一的健康渠道。
func TestManagerLoadsBuiltinLocal(t *testing.T) {
	env := testenv.New(t)
	healthy := env.Manager.Healthy()
	if len(healthy) != 1 || healthy[0].Name != "local" {
		t.Fatalf("健康渠道 = %+v, 期望仅内置 local", healthy)
	}
	local := healthy[0]
	// 默认 local 的存储路径应落在 dataDir/blobs 下。
	payload := []byte("builtin-local-object")
	if err := local.Driver.Put(t.Context(), "probe.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("写入默认渠道: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.DataDir, "blobs", "probe.bin")); err != nil {
		t.Fatalf("对象未落在 dataDir/blobs: %v", err)
	}
}

// TestManagerHealthyByPriority 验证健康渠道按优先级降序,优先级最高者在首位。
func TestManagerHealthyByPriority(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "backup-high", 200, false)

	if got := env.Manager.Healthy(); len(got) != 2 || got[0].ID != id {
		t.Fatalf("Healthy() 顺序异常: %+v, 期望优先级更高的 %d 在前", got, id)
	}
	if got := env.Manager.All(); len(got) != 2 || got[0].ID != id {
		t.Fatalf("All() 顺序异常: %+v", got)
	}
}

// TestManagerHealthySkipsUnhealthy 验证不健康渠道不在健康渠道之列。
func TestManagerHealthySkipsUnhealthy(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "backup-high", 200, false)
	if err := env.Queries.UpdateChannelProbeResult(t.Context(), sqlc.UpdateChannelProbeResultParams{
		Healthy: 0, ID: id,
	}); err != nil {
		t.Fatalf("标记渠道不健康: %v", err)
	}
	if err := env.Manager.Reload(t.Context()); err != nil {
		t.Fatalf("重载管理器: %v", err)
	}
	if healthy := env.Manager.Healthy(); len(healthy) != 1 || healthy[0].Name != "local" {
		t.Fatalf("健康渠道 = %+v, 期望只剩 local", healthy)
	}
}

// TestManagerEncryptedChannel 验证加密渠道:落盘为密文与加密文件名,经驱动读回为明文。
func TestManagerEncryptedChannel(t *testing.T) {
	env := testenv.New(t)
	id, dir := env.AddLocalChannel(t, "vault", 200, true)

	vault, ok := env.Manager.Get(id)
	if !ok {
		t.Fatal("加密渠道未装载")
	}
	plain := []byte("plaintext-image-bytes-0123456789")
	if err := vault.Driver.Put(t.Context(), "img.bin", bytes.NewReader(plain), int64(len(plain))); err != nil {
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
	rc, err := vault.Driver.Open(t.Context(), "img.bin")
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

// TestEncryptedChannelReadableByAnotherManager 验证加密渠道的密钥只由主密钥与渠道记录派生:
// 同库同主密钥再建一个管理器(另一进程或重启)读回仍是明文;换一把主密钥则该渠道配置解不开、
// 被跳过不装载,其余渠道照常可用。
func TestEncryptedChannelReadableByAnotherManager(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "vault", 200, true)
	writer, ok := env.Manager.Get(id)
	if !ok {
		t.Fatal("创建后取不到加密渠道句柄")
	}
	plain := []byte("plaintext-shared-across-processes-0123456789")
	if err := writer.Driver.Put(t.Context(), "img.bin", bytes.NewReader(plain), int64(len(plain))); err != nil {
		t.Fatalf("写入加密渠道: %v", err)
	}

	reader, ok := env.NewManager(t).Get(id)
	if !ok {
		t.Fatal("第二个管理器未装载加密渠道")
	}
	rc, err := reader.Driver.Open(t.Context(), "img.bin")
	if err != nil {
		t.Fatalf("第二个管理器读回对象: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		t.Fatalf("关闭对象流: %v", cerr)
	}
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("第二个管理器读回明文不一致: err=%v", err)
	}

	other, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成另一把主密钥: %v", err)
	}
	stranger, err := channel.NewManager(t.Context(), channel.ManagerConfig{
		Conn: env.Conn, Queries: env.Queries, Master: other, DataDir: env.DataDir, Logger: env.Logger,
	})
	if err != nil {
		t.Fatalf("换主密钥建管理器应只跳过解不开的渠道: %v", err)
	}
	t.Cleanup(func() {
		if cerr := stranger.Close(context.Background()); cerr != nil {
			t.Errorf("关闭管理器: %v", cerr)
		}
	})
	if _, ok := stranger.Get(id); ok {
		t.Fatal("换主密钥后加密渠道不应被装载")
	}
	if healthy := stranger.Healthy(); len(healthy) != 1 || healthy[0].Name != "local" {
		t.Fatalf("换主密钥后健康渠道 = %+v, 期望内置 local 照常可用", healthy)
	}
}

// TestManagerCandidatesForBlob 验证按 blob 的候选渠道:仅健康且副本完成者,按优先级排序。
func TestManagerCandidatesForBlob(t *testing.T) {
	env := testenv.New(t)
	highID, _ := env.AddLocalChannel(t, "backup-high", 300, false)

	// 造一个 blob:副本只在内置 local(done),高优先级渠道为 pending。
	blobID, err := env.Queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "ab", Phash: 1, StorageKey: "k", Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob: %v", err)
	}
	local := env.Local(t)
	mustUpsert := func(chID int64, state string) {
		t.Helper()
		if err := env.Queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: chID, State: state,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	mustUpsert(local.ID, "done")
	mustUpsert(highID, "pending")

	got, err := env.Manager.CandidatesForBlob(t.Context(), blobID)
	if err != nil {
		t.Fatalf("CandidatesForBlob 返回错误: %v", err)
	}
	if len(got) != 1 || got[0].ID != local.ID {
		t.Fatalf("候选 = %+v, 期望仅内置 local(唯一 done 副本)", got)
	}

	// 高优先级副本完成后,应排在候选首位。
	mustUpsert(highID, "done")
	got, err = env.Manager.CandidatesForBlob(t.Context(), blobID)
	if err != nil {
		t.Fatalf("CandidatesForBlob 返回错误: %v", err)
	}
	if len(got) != 2 || got[0].ID != highID {
		t.Fatalf("候选顺序 = %+v, 期望高优先级在前", got)
	}
}

// TestManagerGet 验证按 ID 取句柄。
func TestManagerGet(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "backup-high", 200, false)
	if h, ok := env.Manager.Get(id); !ok || h.Name != "backup-high" {
		t.Fatalf("Get(%d) = %+v/%v, 期望命中 backup-high", id, h, ok)
	}
	if _, ok := env.Manager.Get(99999); ok {
		t.Fatal("Get(不存在的 ID) 应返回 false")
	}
}

// TestManagerCheckAll 验证健康探测:渠道故障被感知、恢复后回归。
func TestManagerCheckAll(t *testing.T) {
	testenv.SkipIfRoot(t)
	env := testenv.New(t)
	id, dir := env.AddLocalChannel(t, "flaky", 300, false)

	changed, err := env.Manager.CheckAll(t.Context())
	if err != nil || len(changed) != 0 {
		t.Fatalf("初始探测 = %v/%v, 期望无变化", changed, err)
	}

	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("模拟渠道故障: %v", err)
	}
	defer func() {
		if cerr := os.Chmod(dir, 0o700); cerr != nil {
			t.Errorf("恢复目录权限: %v", cerr)
		}
	}()
	changed, err = env.Manager.CheckAll(t.Context())
	if err != nil || len(changed) != 1 || changed[0] != "flaky" {
		t.Fatalf("故障探测 = %v/%v, 期望 flaky 状态翻转", changed, err)
	}
	if first := firstHealthy(t, env.Manager); first.Name != "local" {
		t.Fatalf("故障后首选渠道 = %q, 期望降级回 local", first.Name)
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("恢复渠道: %v", err)
	}
	changed, err = env.Manager.CheckAll(t.Context())
	if err != nil || len(changed) != 1 {
		t.Fatalf("恢复探测 = %v/%v, 期望 flaky 回归", changed, err)
	}
	if first := firstHealthy(t, env.Manager); first.Name != "flaky" {
		t.Fatalf("恢复后首选渠道 = %q, 期望 flaky", first.Name)
	}
	if _, ok := env.Manager.Get(id); !ok {
		t.Fatal("恢复后渠道句柄丢失")
	}
}

// TestManagerNoHealthyChannel 验证全部不健康时没有可选的健康渠道。
func TestManagerNoHealthyChannel(t *testing.T) {
	env := testenv.New(t)
	for _, h := range env.Manager.All() {
		if err := env.Queries.UpdateChannelProbeResult(t.Context(), sqlc.UpdateChannelProbeResultParams{
			Healthy: 0, ID: h.ID,
		}); err != nil {
			t.Fatalf("标记不健康: %v", err)
		}
	}
	if err := env.Manager.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if healthy := env.Manager.Healthy(); len(healthy) != 0 {
		t.Fatalf("健康渠道 = %+v, 期望为空", healthy)
	}
}

// TestReloadReusesUnchangedDriver 验证 Reload 只重建配置有变的渠道:无变化时驱动实例沿用,
// 编辑配置后该渠道换新实例、其他渠道不动。
func TestReloadReusesUnchangedDriver(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "stable", 200, false)
	before, _ := env.Manager.Get(id)
	local := env.Local(t)

	if err := env.Manager.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if after, _ := env.Manager.Get(id); after.Driver != before.Driver {
		t.Fatal("配置未变的渠道被重建了驱动实例")
	}

	if err := env.Manager.Update(t.Context(), id, channel.UpdateParams{
		Name: "stable", Driver: "local", Config: map[string]string{"path": t.TempDir()}, Priority: 200,
	}); err != nil {
		t.Fatalf("编辑渠道: %v", err)
	}
	if after, _ := env.Manager.Get(id); after.Driver == before.Driver {
		t.Fatal("配置已变的渠道仍沿用旧驱动实例")
	}
	if l, _ := env.Manager.Get(local.ID); l.Driver != local.Driver {
		t.Fatal("编辑一个渠道波及了另一个渠道的驱动实例")
	}
}

// TestCheckAllAbandonsOnCancel 验证 ctx 取消时整轮探测作废:不返回翻转、不落库——
// 停机瞬间的探测失败是取消造成的,不能把渠道记成不健康。
func TestCheckAllAbandonsOnCancel(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "steady", 200, false)
	before := mustGetChannel(t, env, id)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	changed, err := env.Manager.CheckAll(ctx)
	if !errors.Is(err, context.Canceled) || len(changed) != 0 {
		t.Fatalf("取消后 CheckAll = %v/%v, 期望仅返回取消错误", changed, err)
	}
	after := mustGetChannel(t, env, id)
	if after.Healthy != before.Healthy || after.LastCheckedAt != before.LastCheckedAt {
		t.Fatalf("取消的探测仍落库: %+v → %+v", before, after)
	}
	if first := firstHealthy(t, env.Manager); first.ID != id {
		t.Fatalf("取消的探测改动了内存健康标记: 首选渠道 %+v", first)
	}
}

// TestDeleteWithPurgeMarksDraining 验证带清理的删除只打标记:渠道退出选路、补齐、探测与列表的在役集合,
// Get 仍可取到并标明清理中;Remove 收尾后彻底消失。不带清理的删除立即删行。
func TestDeleteWithPurgeMarksDraining(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "drain-me", 300, false)

	if err := env.Manager.Delete(t.Context(), id, true); err != nil {
		t.Fatalf("带清理删除: %v", err)
	}
	h, ok := env.Manager.Get(id)
	if !ok || !h.PendingDelete {
		t.Fatalf("Get = %+v/%v, 期望仍可取到且标明清理中", h, ok)
	}
	for _, c := range env.Manager.All() {
		if c.ID == id {
			t.Fatal("清理中渠道不应出现在在役集合 All()")
		}
	}
	for _, c := range env.Manager.Healthy() {
		if c.ID == id {
			t.Fatal("清理中渠道不应出现在健康渠道之列")
		}
	}
	if draining := env.Manager.Draining(); len(draining) != 1 || draining[0].ID != id {
		t.Fatalf("Draining() = %+v, 期望仅含清理中渠道", draining)
	}
	list, err := env.Manager.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var info channel.Info
	for _, c := range list {
		if c.ID == id {
			info = c
		}
	}
	if !info.Deleting {
		t.Fatalf("列表项未标明清理中: %+v", info)
	}
	before := mustGetChannel(t, env, id)
	if _, err := env.Manager.CheckAll(t.Context()); err != nil {
		t.Fatalf("CheckAll: %v", err)
	}
	if after := mustGetChannel(t, env, id); after.LastCheckedAt != before.LastCheckedAt {
		t.Fatal("清理中渠道不应再被探测")
	}

	if err := env.Manager.Remove(t.Context(), id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := env.Manager.Get(id); ok {
		t.Fatal("Remove 后渠道仍可取到")
	}

	id2, _ := env.AddLocalChannel(t, "drop-now", 200, false)
	if err := env.Manager.Delete(t.Context(), id2, false); err != nil {
		t.Fatalf("不带清理删除: %v", err)
	}
	if _, ok := env.Manager.Get(id2); ok {
		t.Fatal("不带清理的删除应立即移除渠道")
	}
}

// TestDeleteRefusesWhenOnlyOtherCopyIsDraining 验证唯一副本判定不把清理中渠道当作可替代副本:
// 两个渠道先后带清理删除,第二个必须被拒,否则同一张图的副本会被删光。
func TestDeleteRefusesWhenOnlyOtherCopyIsDraining(t *testing.T) {
	env := testenv.New(t)
	first, _ := env.AddLocalChannel(t, "copy-a", 300, false)
	second, _ := env.AddLocalChannel(t, "copy-b", 200, false)
	blobID := mustCreateBlob(t, env, "shared-hash", "shared_00000000")
	for _, chID := range []int64{first, second} {
		if err := env.Queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: chID, State: channel.StateDone,
		}); err != nil {
			t.Fatalf("写副本状态: %v", err)
		}
	}
	if err := env.Manager.Delete(t.Context(), first, true); err != nil {
		t.Fatalf("删除第一个渠道: %v", err)
	}
	if err := env.Manager.Delete(t.Context(), second, true); !errors.Is(err, channel.ErrHoldsSoleReplicas) {
		t.Fatalf("删除第二个渠道错误 = %v, 期望 ErrHoldsSoleReplicas(另一份副本在清理中渠道上)", err)
	}
}
