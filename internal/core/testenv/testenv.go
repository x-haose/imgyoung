// Package testenv 提供 core 各包测试共用的装配:临时库、主密钥与已完成首次加载的渠道管理器,
// 消除各包测试里重复的"建库 → 生成主密钥 → 建管理器"样板。只供测试代码导入。
package testenv

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// Env 一套测试装配;库、管理器随测试结束关闭。
type Env struct {
	Conn    *sql.DB           // 数据库写句柄,供服务装配(事务)与用例直写 SQL
	Queries *sqlc.Queries     // 查询集,经读写路由句柄构造
	Master  *secret.MasterKey // 主密钥;同一密钥可再建第二个管理器模拟另一进程
	DataDir string            // 数据目录;迁移播种的内置 local 渠道落 DataDir/blobs
	Manager *channel.Manager  // 渠道管理器,已加载内置 local 渠道
	Logger  *slog.Logger      // 丢弃式日志,供各服务装配
}

// New 建库迁移、生成主密钥并创建渠道管理器。
// @param t 测试上下文,失败即 Fatal
// @return 就绪的装配
func New(t *testing.T) *Env {
	t.Helper()
	conn, queries := dbtest.New(t)
	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	env := &Env{
		Conn: conn, Queries: queries, Master: master, DataDir: t.TempDir(),
		Logger: slog.New(slog.DiscardHandler),
	}
	env.Manager = env.NewManager(t)
	return env
}

// NewManager 在同一库与主密钥上再建一个渠道管理器,模拟另一进程读取同一份配置;随测试结束关闭。
// @param t 测试上下文,失败即 Fatal
// @return 已完成首次加载的管理器
func (e *Env) NewManager(t *testing.T) *channel.Manager {
	t.Helper()
	mgr, err := channel.NewManager(t.Context(), channel.ManagerConfig{
		Conn: e.Conn, Queries: e.Queries, Master: e.Master, DataDir: e.DataDir, Logger: e.Logger,
	})
	if err != nil {
		t.Fatalf("创建渠道管理器: %v", err)
	}
	t.Cleanup(func() {
		if cerr := mgr.Close(context.Background()); cerr != nil {
			t.Errorf("关闭渠道管理器: %v", cerr)
		}
	})
	return mgr
}

// AddLocalChannel 经管理器创建一条指向独立临时目录的 local 渠道。
// @param t 测试上下文,失败即 Fatal
// @param name 渠道名
// @param priority 优先级,越大越优先;内置 local 为 100
// @param encrypted 是否加密渠道
// @return 渠道 ID
// @return 渠道存储目录
func (e *Env) AddLocalChannel(t *testing.T, name string, priority int64, encrypted bool) (int64, string) {
	t.Helper()
	dir := t.TempDir()
	id, err := e.Manager.Create(t.Context(), channel.CreateParams{
		Name: name, Driver: "local", Config: map[string]string{"path": dir},
		Priority: priority, Encrypted: encrypted,
	})
	if err != nil {
		t.Fatalf("创建渠道 %q: %v", name, err)
	}
	return id, dir
}

// Settings 返回在出厂默认值上按 mod 覆写后的运营配置读取口;mod 为 nil 即取默认。
// @param mod 覆写钩子,改动默认配置副本
// @return 逐次取值的配置读取口,每次返回同一份快照
func Settings(mod func(*config.Settings)) func() config.Settings {
	set := config.Defaults()
	if mod != nil {
		mod(&set)
	}
	return func() config.Settings { return set }
}

// SkipIfRoot 以 root 运行时跳过依赖文件权限制造故障的用例:root 不受权限位约束,故障造不出来。
// @param t 测试上下文
func SkipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root 不受文件权限约束,无法用权限位模拟故障")
	}
}
