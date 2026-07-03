// Package db 提供数据库连接、schema 迁移与 sqlc 生成的类型安全查询。
// 本文件为手写代码;db.go/models.go/queries.sql.go 由 sqlc 生成,勿手改。
package db

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"

	// 注册 modernc 纯 Go SQLite 驱动(driver 名 "sqlite",保住 CGO_ENABLED=0)。
	_ "modernc.org/sqlite"
)

// migrationsFS 打进二进制的迁移文件(goose + go:embed)。
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open 打开(必要时创建)SQLite 数据库并完成连接配置。
// @Description: DSN 内置三个 pragma——WAL(读写并发)、busy_timeout 5s(单写者
// 竞争时等待而非立即报错)、foreign_keys(引用完整性)。
// @param path SQLite 数据库文件路径
// @return 就绪的连接池
// @return 打开或连通性校验失败时的错误
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		path,
	)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %q: %w", path, err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库 %q: %w", path, err)
	}
	return conn, nil
}

// Migrate 将数据库 schema 迁移到最新版本。
// @Description: 迁移文件来自内嵌 FS,启动时调用,幂等。
// @param conn 已打开的数据库连接
// @return 迁移失败时的错误
func Migrate(conn *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("设置 goose 方言: %w", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		return fmt.Errorf("执行 schema 迁移: %w", err)
	}
	return nil
}
