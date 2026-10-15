package channel

import (
	"context"
	"log/slog"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// TestPersistParamUpdatesConfigWithoutRebuild 验证渠道自己驱动的写回并入渠道配置密文落库,
// 且同步到驱动输入——随后的 Reload 不把写回误判为配置变更而重建驱动;一次性驱动的写回只落库,
// 随后的 Reload 重建在用驱动换用新值。
func TestPersistParamUpdatesConfigWithoutRebuild(t *testing.T) {
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
	before, _ := m.Get(id)

	m.persistOwn(id, "token", `{"access_token":"new"}`)

	row, err := queries.GetChannel(t.Context(), id)
	if err != nil {
		t.Fatalf("读取渠道: %v", err)
	}
	params, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		t.Fatalf("解密配置: %v", err)
	}
	if params["token"] != `{"access_token":"new"}` {
		t.Fatalf("写回未落库: %v", params)
	}
	if err := m.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if after, _ := m.Get(id); after.Driver != before.Driver {
		t.Fatal("写回后 Reload 重建了驱动实例")
	}

	if !m.persistParam(id, "token", `{"access_token":"trial"}`) {
		t.Fatal("一次性驱动的写回未落库")
	}
	if err := m.Reload(t.Context()); err != nil {
		t.Fatalf("重载: %v", err)
	}
	if after, _ := m.Get(id); after.Driver == before.Driver {
		t.Fatal("一次性驱动写回后 Reload 未重建在用驱动")
	}
}
