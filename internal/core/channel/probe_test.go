package channel_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// TestManagerUpdateChannel 验证编辑渠道:名称/优先级/配置整体替换生效,
// 驱动实例重建(fs 缓存失效),并且编辑后深探测结果落库。
func TestManagerUpdateChannel(t *testing.T) {
	env := testenv.New(t)
	id, oldDir := env.AddLocalChannel(t, "edit-me", 50, false)

	newDir := t.TempDir()
	if err := env.Manager.Update(t.Context(), id, channel.UpdateParams{
		Name: "edited", Driver: "local",
		Config: map[string]string{"path": newDir}, Priority: 300,
	}); err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}

	list, err := env.Manager.List(t.Context())
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	var got channel.Info
	for _, c := range list {
		if c.ID == id {
			got = c
		}
	}
	if got.Name != "edited" || got.Priority != 300 {
		t.Fatalf("编辑未生效: %+v", got)
	}
	if got.LastCheckedAt == "" || got.Failure != nil || !got.Healthy {
		t.Fatalf("编辑后深探测结果异常: %+v", got)
	}

	// 驱动已重建:编辑后的渠道写入应落到新目录而非旧目录。
	edited, ok := env.Manager.Get(id)
	if !ok {
		t.Fatal("编辑后的渠道未装载")
	}
	payload := []byte("post-update-object")
	if err := edited.Driver.Put(t.Context(), "after.bin", strings.NewReader(string(payload)),
		int64(len(payload))); err != nil {
		t.Fatalf("写入编辑后渠道: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "after.bin")); err != nil {
		t.Fatalf("对象未落到新配置目录(fs 缓存未失效): %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "after.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("对象落到了旧配置目录: %v", err)
	}
}

// TestManagerUpdateGuards 验证编辑守卫:渠道不存在与加密属性翻转分别被明确拒绝。
func TestManagerUpdateGuards(t *testing.T) {
	env := testenv.New(t)
	id, dir := env.AddLocalChannel(t, "guard", 50, false)

	if err := env.Manager.Update(t.Context(), 99999, channel.UpdateParams{
		Name: "x", Driver: "local", Config: map[string]string{"path": dir},
	}); !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("编辑不存在渠道错误 = %v, 期望 ErrChannelNotFound", err)
	}

	flip := true
	if err := env.Manager.Update(t.Context(), id, channel.UpdateParams{
		Name: "guard", Driver: "local", Config: map[string]string{"path": dir}, Encrypted: &flip,
	}); !errors.Is(err, channel.ErrEncryptedImmutable) {
		t.Fatalf("翻转加密属性错误 = %v, 期望 ErrEncryptedImmutable", err)
	}

	// 显式回传与现状一致的 encrypted 应放行。
	same := false
	if err := env.Manager.Update(t.Context(), id, channel.UpdateParams{
		Name: "guard", Driver: "local", Config: map[string]string{"path": dir}, Encrypted: &same,
	}); err != nil {
		t.Fatalf("回传一致 encrypted 应放行: %v", err)
	}
}

// TestManagerTestConfig 验证测试连接:好配置探测通过且不残留探测对象;
// 坏配置返回人话化错误且凭证绝不回显。
func TestManagerTestConfig(t *testing.T) {
	env := testenv.New(t)

	dir := t.TempDir()
	res, err := env.Manager.TestConfig(t.Context(), channel.TestParams{
		Driver: "local", Config: map[string]string{"path": dir},
	})
	if err != nil || res.Failure != nil {
		t.Fatalf("好配置探测失败: %+v / %v", res.Failure, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读探测目录: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("探测对象未清理: %v", entries)
	}

	const secret = "SUPERSECRETVALUE0123"
	res, err = env.Manager.TestConfig(t.Context(), channel.TestParams{Driver: "s3", Config: map[string]string{
		"provider": "Minio", "endpoint": "http://127.0.0.1:1",
		"access_key_id": "AK", "secret_access_key": secret, "path": "bkt",
	}})
	if err != nil {
		t.Fatalf("探测失败不应算方法失败: %v", err)
	}
	if res.Failure == nil {
		t.Fatal("不可达端点应探测失败")
	}
	if strings.Contains(res.Failure.Detail, secret) {
		t.Fatalf("失败原文回显了凭证: %s", res.Failure.Detail)
	}
	if _, err := env.Manager.TestConfig(t.Context(), channel.TestParams{
		ChannelID: 99999, Driver: "local", Config: map[string]string{"path": dir},
	}); !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("编辑场景指向不存在的渠道错误 = %v, 期望 ErrChannelNotFound", err)
	}
}

// TestLoadFailureVisibleAndRetried 验证装载失败如实落库并会被重试:配置解不开的渠道在列表里显示
// 不健康与「装载失败」原因、不参与选路;配置恢复后,手动重检与周期健康检查都会把它装上并探测。
func TestLoadFailureVisibleAndRetried(t *testing.T) {
	env := testenv.New(t)
	id, _ := env.AddLocalChannel(t, "fragile", 300, false)
	good := mustGetChannel(t, env, id).ConfigSealed
	setSealedConfig := func(sealed string) {
		t.Helper()
		if err := env.Queries.UpdateChannelConfigSealed(t.Context(), sqlc.UpdateChannelConfigSealedParams{
			ConfigSealed: sealed, ID: id,
		}); err != nil {
			t.Fatalf("改写渠道配置密文: %v", err)
		}
	}
	assertLoaded := func(want bool) {
		t.Helper()
		row := mustGetChannel(t, env, id)
		_, loaded := env.Manager.Get(id)
		selectable := slices.ContainsFunc(env.Manager.Healthy(), func(h channel.Handle) bool { return h.ID == id })
		if loaded != want || (row.Healthy == 1) != want || selectable != want {
			t.Fatalf("装载=%v 健康=%d 参与选路=%v, 期望三者均为 %v", loaded, row.Healthy, selectable, want)
		}
		if !want && (row.LastErrorStage.String != string(channel.StageLoad) ||
			row.LastErrorReason.String != string(channel.ReasonConfigUnreadable)) {
			t.Fatalf("失败栏 = %s/%s, 期望装载环节的配置无法读取", row.LastErrorStage.String, row.LastErrorReason.String)
		}
	}

	setSealedConfig("not-a-sealed-config")
	if _, err := env.Manager.CheckAll(t.Context()); err != nil {
		t.Fatalf("健康检查: %v", err)
	}
	assertLoaded(false)
	setSealedConfig(good)
	if err := env.Manager.ProbeChannel(t.Context(), id); err != nil {
		t.Fatalf("手动重检: %v", err)
	}
	assertLoaded(true)

	setSealedConfig("not-a-sealed-config")
	if err := env.Manager.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	assertLoaded(false)
	setSealedConfig(good)
	if changed, err := env.Manager.CheckAll(t.Context()); err != nil || !slices.Contains(changed, "fragile") {
		t.Fatalf("健康检查 = %v/%v, 期望重试装载后 fragile 翻回健康", changed, err)
	}
	assertLoaded(true)
}

// TestManagerCheckAllPersistsProbeResult 验证轻探测落库:每轮更新 last_checked_at,
// 失败存失败结论(环节、归类、原文),恢复后清空。
func TestManagerCheckAllPersistsProbeResult(t *testing.T) {
	testenv.SkipIfRoot(t)
	env := testenv.New(t)
	id, dir := env.AddLocalChannel(t, "flaky", 300, false)

	if _, err := env.Manager.CheckAll(t.Context()); err != nil {
		t.Fatalf("初始探测: %v", err)
	}
	row := mustGetChannel(t, env, id)
	if !row.LastCheckedAt.Valid || row.LastError.Valid {
		t.Fatalf("健康渠道探测落库异常: %+v", row)
	}

	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("模拟渠道故障: %v", err)
	}
	defer func() {
		if cerr := os.Chmod(dir, 0o700); cerr != nil {
			t.Errorf("恢复目录权限: %v", cerr)
		}
	}()
	if _, err := env.Manager.CheckAll(t.Context()); err != nil {
		t.Fatalf("故障探测: %v", err)
	}
	row = mustGetChannel(t, env, id)
	if row.Healthy != 0 || row.LastErrorStage.String != string(channel.StageProbe) ||
		row.LastErrorReason.String != string(channel.ReasonPermission) || row.LastError.String == "" {
		t.Fatalf("故障未按无写入权限落库: %+v", row)
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("恢复渠道: %v", err)
	}
	if _, err := env.Manager.CheckAll(t.Context()); err != nil {
		t.Fatalf("恢复探测: %v", err)
	}
	row = mustGetChannel(t, env, id)
	if row.Healthy != 1 || row.LastError.Valid || row.LastErrorStage.Valid || row.LastErrorReason.Valid {
		t.Fatalf("恢复后失败栏未清空: %+v", row)
	}
}

// TestManagerCheckAllKeepsDeepProbeFailure 验证轻探测只降不升:深探测判定的
// 失败(可读不可写)不被只读轻探测推翻,恢复写权限后经深探测确认才翻回健康。
func TestManagerCheckAllKeepsDeepProbeFailure(t *testing.T) {
	testenv.SkipIfRoot(t)
	env := testenv.New(t)
	id, dir := env.AddLocalChannel(t, "readonly", 300, false)

	// 目录只读:轻探测(只读 Stat)通过,深探测(写)失败。
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("模拟只读渠道: %v", err)
	}
	defer func() {
		if cerr := os.Chmod(dir, 0o700); cerr != nil {
			t.Errorf("恢复目录权限: %v", cerr)
		}
	}()
	if err := env.Manager.ProbeChannel(t.Context(), id); err != nil {
		t.Fatalf("深探测: %v", err)
	}
	row := mustGetChannel(t, env, id)
	if row.Healthy != 0 || !row.LastError.Valid {
		t.Fatalf("深探测未记录写失败: %+v", row)
	}

	// 周期轻探测不得复活假健康。
	if _, err := env.Manager.CheckAll(t.Context()); err != nil {
		t.Fatalf("轻探测轮询: %v", err)
	}
	row = mustGetChannel(t, env, id)
	if row.Healthy != 0 || !row.LastError.Valid || row.LastError.String == "" {
		t.Fatalf("轻探测推翻了深探测失败结论: %+v", row)
	}

	// 恢复写权限后,轻探测轮询经深探测确认翻回健康。
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("恢复渠道: %v", err)
	}
	changed, err := env.Manager.CheckAll(t.Context())
	if err != nil || len(changed) != 1 || changed[0] != "readonly" {
		t.Fatalf("恢复探测 = %v/%v, 期望 readonly 回归", changed, err)
	}
	row = mustGetChannel(t, env, id)
	if row.Healthy != 1 || row.LastError.Valid {
		t.Fatalf("恢复后未翻回健康: %+v", row)
	}
}

// TestManagerCreateSeedsAndProbes 验证创建渠道:立即深探测落库,并为存量 blob
// 播种 pending 副本行(排除 pending_delete 的 blob)。
func TestManagerCreateSeedsAndProbes(t *testing.T) {
	env := testenv.New(t)

	keepID := mustCreateBlob(t, env, "keep-hash", "keep_00000000")
	goneID := mustCreateBlob(t, env, "gone-hash", "gone_00000000")
	if err := env.Queries.MarkBlobMergedAway(t.Context(), sqlc.MarkBlobMergedAwayParams{
		DeleteAfter: sql.NullString{String: "2026-01-01 00:00:00", Valid: true}, ID: goneID,
	}); err != nil {
		t.Fatalf("标记 blob 待删: %v", err)
	}

	id, err := env.Manager.Create(t.Context(), channel.CreateParams{
		Name: "seeded", Driver: "local",
		Config: map[string]string{"path": t.TempDir()}, Priority: 10,
	})
	if err != nil {
		t.Fatalf("创建渠道: %v", err)
	}

	row := mustGetChannel(t, env, id)
	if !row.LastCheckedAt.Valid || row.Healthy != 1 {
		t.Fatalf("创建后未立即探测落库: %+v", row)
	}

	bc, err := env.Queries.GetBlobChannel(t.Context(), sqlc.GetBlobChannelParams{BlobID: keepID, ChannelID: id})
	if err != nil {
		t.Fatalf("存量 blob 未播种副本行: %v", err)
	}
	if bc.State != channel.StatePending {
		t.Fatalf("播种状态 = %q, 期望 pending", bc.State)
	}
	if _, err := env.Queries.GetBlobChannel(t.Context(), sqlc.GetBlobChannelParams{
		BlobID: goneID, ChannelID: id,
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("pending_delete 的 blob 不应播种: %v", err)
	}
}

// mustGetChannel 读取渠道行,失败即终止测试。
func mustGetChannel(t *testing.T, env *testenv.Env, id int64) sqlc.Channel {
	t.Helper()
	row, err := env.Queries.GetChannel(t.Context(), id)
	if err != nil {
		t.Fatalf("读取渠道 %d: %v", id, err)
	}
	return row
}

// mustCreateBlob 创建一条最小 blob 记录,失败即终止测试。
// @return blob 主键
func mustCreateBlob(t *testing.T, env *testenv.Env, sha, key string) int64 {
	t.Helper()
	id, err := env.Queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: sha, Phash: 1, StorageKey: key, Mime: "image/png", Size: 1, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("创建 blob: %v", err)
	}
	return id
}
