package channel

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// TestPersistParamUpdatesConfigWithoutRebuild 验证渠道自己驱动的写回并入渠道配置密文落库,
// 且同步到驱动输入——随后的 Reload 不把写回误判为配置变更而重建驱动;一次性驱动的写回只落库,
// 随后的 Reload 重建在用驱动换用新值;库里的值已被改掉之后,旧驱动的写回不再覆盖。
func TestPersistParamUpdatesConfigWithoutRebuild(t *testing.T) {
	m, id := newLocalManager(t)
	before, _ := m.Get(id)
	live := liveOf(m, id)

	m.persistOwn(id, live, "token", `{"access_token":"new"}`)

	if got := storedParam(t, m, id, "token"); got != `{"access_token":"new"}` {
		t.Fatalf("写回未落库: %s", got)
	}
	if err := m.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if after, _ := m.Get(id); after.Driver != before.Driver {
		t.Fatal("写回后 Reload 重建了驱动实例")
	}

	if !m.persistParam(id, "token", `{"access_token":"new"}`, `{"access_token":"trial"}`) {
		t.Fatal("一次性驱动的写回未落库")
	}
	m.persistOwn(id, live, "token", `{"access_token":"stale"}`)
	if got := storedParam(t, m, id, "token"); got != `{"access_token":"trial"}` {
		t.Fatalf("库里的值已被改掉后旧驱动的写回仍覆盖了它: %s", got)
	}
	if err := m.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if after, _ := m.Get(id); after.Driver == before.Driver {
		t.Fatal("一次性驱动写回后 Reload 未重建在用驱动")
	}
}

// TestEditRejectedAfterConcurrentWriteBack 验证编辑读到配置之后、落库之前渠道配置被写回改动时,编辑不落库并报
// 配置已改动,写回的值保留——否则编辑会用读到的旧令牌盖掉刷新后的令牌。
func TestEditRejectedAfterConcurrentWriteBack(t *testing.T) {
	m, id := newLocalManager(t)
	row, err := m.cfg.Queries.GetChannel(t.Context(), id)
	if err != nil {
		t.Fatalf("读取渠道: %v", err)
	}
	old, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		t.Fatalf("解密配置: %v", err)
	}
	const refreshed = `{"access_token":"refreshed"}`
	if applied, err := m.mergeParam(t.Context(), id, "token", "", refreshed); err != nil || !applied {
		t.Fatalf("写回: applied=%v err=%v", applied, err)
	}

	config := maps.Clone(old)
	err = m.applyEdit(t.Context(), row, UpdateParams{Name: "renamed", Driver: "local", Config: config}, old, config)
	if !errors.Is(err, ErrChannelConfigChanged) {
		t.Fatalf("编辑应报配置已改动, got %v", err)
	}
	if got := storedParam(t, m, id, "token"); got != refreshed {
		t.Fatalf("写回的令牌被编辑覆盖: %s", got)
	}
}

// TestWriteBackRecoversAfterFailedPersist 验证写回落库失败一次之后,同一驱动的下一次写回照常落库——
// 比较基准停在库里实际的值,不被那次没落库的新值带偏。
func TestWriteBackRecoversAfterFailedPersist(t *testing.T) {
	m, id := newLocalManager(t)
	live := liveOf(m, id)
	exec := func(stmt string) {
		t.Helper()
		if _, err := m.cfg.Conn.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	exec(`CREATE TRIGGER fail_config BEFORE UPDATE OF config_sealed ON channels
		BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	m.persistOwn(id, live, "token", `{"access_token":"T2"}`)
	exec(`DROP TRIGGER fail_config`)
	m.persistOwn(id, live, "token", `{"access_token":"T3"}`)
	if got := storedParam(t, m, id, "token"); got != `{"access_token":"T3"}` {
		t.Fatalf("落库恢复后的写回没有落库: %s", got)
	}
}

// newLocalManager 建一个只有一条 local 渠道的管理器,测试结束自动关闭。
// @return 管理器与渠道主键
func newLocalManager(t *testing.T) (*Manager, int64) {
	t.Helper()
	conn, queries := dbtest.New(t)
	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	m, err := NewManager(t.Context(), ManagerConfig{
		Conn: conn, Queries: queries, Master: master, DataDir: t.TempDir(),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	id, err := m.Create(t.Context(), CreateParams{
		Name: "drive-like", Driver: "local", Config: map[string]string{"path": t.TempDir()},
	})
	if err != nil {
		t.Fatalf("创建渠道: %v", err)
	}
	return m, id
}

// liveOf 取已装载渠道的驱动参数值表。
func liveOf(m *Manager, channelID int64) *liveParams {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.channels {
		if c.handle.ID == channelID {
			return c.live
		}
	}
	return nil
}

// storedParam 读出渠道库里某个参数的明文值。
func storedParam(t *testing.T, m *Manager, channelID int64, key string) string {
	t.Helper()
	row, err := m.cfg.Queries.GetChannel(t.Context(), channelID)
	if err != nil {
		t.Fatalf("读取渠道: %v", err)
	}
	params, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		t.Fatalf("解密配置: %v", err)
	}
	return params[key]
}
