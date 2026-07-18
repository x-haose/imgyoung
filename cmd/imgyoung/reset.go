// 运维子命令:不进 fx 全量装配,只加载配置、直连 SQLite 完成一次性操作。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// usageText 子命令用法说明,--help 与未知子命令时输出。
const usageText = `ImgYoung 自部署图床

用法:
  imgyoung                          启动服务(无子命令)
  imgyoung reset-password           重置管理员(admin)密码并打印新随机密码
  imgyoung reset-password --user X  重置指定登录名的密码
  imgyoung help                     显示本说明

reset-password 直连数据目录下的 SQLite,服务运行中执行亦安全;
数据目录取自与服务相同的配置(配置文件 / IMGYOUNG_* 环境变量),
须在服务的工作目录下执行,或显式设置 IMGYOUNG_DATA_DIR。
`

// runSubcommand 分发运维子命令。
// @param args 去掉程序名后的命令行参数,首项为子命令名
// @return 进程退出码
func runSubcommand(args []string) int {
	switch args[0] {
	case "reset-password":
		return runResetPassword(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n\n%s", args[0], usageText)
		return 2
	}
}

// runResetPassword 解析 reset-password 参数并执行重置,结果写 stdout、错误写 stderr。
// @param args 子命令参数(--user)
// @return 进程退出码
func runResetPassword(args []string) int {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	user := fs.String("user", "admin", "要重置密码的登录名")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	password, err := resetPassword(context.Background(), *user)
	if err != nil {
		fmt.Fprintln(os.Stderr, "重置失败:", err)
		return 1
	}
	fmt.Printf(`密码已重置。

  用户名: %s
  新密码: %s

请立即用新密码登录后台,并在「设置 → 修改密码」中改为自己的密码;
该用户的既有登录会话已全部失效。
`, *user, password)
	return 0
}

// resetPassword 定位数据库、生成新随机密码并入库(argon2id),吊销该用户全部会话。
// @Description: 只读配置定位 SQLite 并跑迁移确认 schema;数据库文件不存在时拒绝执行,
// 防止在错误的工作目录下静默新建一个空库。用户不存在时列出可用登录名。
// @param ctx 控制超时与取消
// @param name 目标登录名
// @return 新密码明文(仅此一次)
// @return 定位/校验/写入失败时的错误
func resetPassword(ctx context.Context, name string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	dbPath := filepath.Join(cfg.DataDir, "imgyoung.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("未找到数据库 %q,请在服务的工作目录下执行,或设置 IMGYOUNG_DATA_DIR 指向服务数据目录: %w", dbPath, err)
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	if err := db.Migrate(conn); err != nil {
		return "", err
	}
	queries := sqlc.New(conn)
	user, err := queries.GetUserByName(ctx, name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("用户 %q 不存在;可用登录名: %s", name, strings.Join(userNames(ctx, queries), ", "))
	case err != nil:
		return "", fmt.Errorf("查询用户: %w", err)
	}
	password, err := auth.NewRandomPassword()
	if err != nil {
		return "", err
	}
	authSvc := auth.New(auth.Config{
		Conn:    conn,
		Queries: queries,
		Logger:  slog.New(slog.DiscardHandler),
	})
	if err := authSvc.SetPassword(ctx, user.ID, password); err != nil {
		return "", err
	}
	return password, nil
}

// userNames 列出全部登录名,供"用户不存在"的错误提示;查询失败时返回空列表。
// @param ctx 控制超时与取消
// @param queries 查询集
// @return 登录名列表
func userNames(ctx context.Context, queries *sqlc.Queries) []string {
	rows, err := queries.ListUsersWithUsage(ctx)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}
