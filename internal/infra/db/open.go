// Package db 提供数据库连接与 schema 迁移;类型安全查询层见子包 sqlc。
package db

import (
	"database/sql"
	"fmt"

	// 注册 modernc 纯 Go SQLite 驱动(driver 名 "sqlite",保住 CGO_ENABLED=0)。
	_ "modernc.org/sqlite"
)

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
