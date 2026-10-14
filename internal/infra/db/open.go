// Package db 提供数据库连接与 schema 迁移;类型安全查询层见子包 sqlc。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	// 注册 modernc 纯 Go SQLite 驱动(driver 名 "sqlite",保住 CGO_ENABLED=0)。
	_ "modernc.org/sqlite"
)

// pragmas 两个句柄共用的连接参数:WAL(读写并发)、busy_timeout 5s(外部进程
// 写竞争时等待而非立即报错)、foreign_keys(引用完整性)。
const pragmas = "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

// DB 元数据库句柄:写与事务走单连接,读走连接池。
// @Description: SQLite 任一时刻只容一个写者,多连接并发写只会在 busy_timeout 上排队、
// 到点报 SQLITE_BUSY。写句柄限一条连接,写竞争在 Go 侧排队而不撞库;写事务以
// BEGIN IMMEDIATE 开启,先读后写的事务不会在升级写锁时失败。读句柄不限连接数,
// WAL 下读不阻塞写。实现 sqlc 的 DBTX:按语句首个关键字把 SELECT 路由到读句柄、
// 其余路由到写句柄。
type DB struct {
	write *sql.DB // 写句柄,单连接;事务与全部改动语句经此
	read  *sql.DB // 读句柄,连接池;只读查询经此
}

// Open 打开(必要时创建)SQLite 数据库并完成两个句柄的连接配置。
// @param path SQLite 数据库文件路径
// @return 就绪的句柄
// @return 打开或连通性校验失败时的错误
func Open(path string) (*DB, error) {
	write, err := openPool(path, "immediate")
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	read, err := openPool(path, "deferred")
	if err != nil {
		_ = write.Close()
		return nil, err
	}
	return &DB{write: write, read: read}, nil
}

// openPool 以指定的事务锁级打开一个连接池并校验连通性。
// @param path SQLite 数据库文件路径
// @param txlock BeginTx 发出的 BEGIN 变体:immediate(写句柄)或 deferred(读句柄)
// @return 就绪的连接池
// @return 打开或连通性校验失败时的错误
func openPool(path, txlock string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", fmt.Sprintf("file:%s?%s&_txlock=%s", path, pragmas, txlock))
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %q: %w", path, err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("连接数据库 %q: %w", path, err)
	}
	return conn, nil
}

// Writer 返回写句柄,供需要事务的调用方 BeginTx;事务内的读写都经该连接。
// @return 写句柄
func (d *DB) Writer() *sql.DB { return d.write }

// Reader 返回读句柄,供 VACUUM INTO 这类只读但耗时的语句使用,不占写连接。
// @return 读句柄
func (d *DB) Reader() *sql.DB { return d.read }

// Close 关闭两个句柄。
// @return 任一句柄关闭失败时的错误
func (d *DB) Close() error {
	return errors.Join(d.write.Close(), d.read.Close())
}

// ExecContext 执行语句,按语句类型路由句柄。
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.route(query).ExecContext(ctx, query, args...)
}

// PrepareContext 预编译语句,按语句类型路由句柄。
func (d *DB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return d.route(query).PrepareContext(ctx, query)
}

// QueryContext 执行多行查询,按语句类型路由句柄。
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.route(query).QueryContext(ctx, query, args...)
}

// QueryRowContext 执行单行查询,按语句类型路由句柄。
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.route(query).QueryRowContext(ctx, query, args...)
}

// route 按语句类型选择句柄。
// @param query SQL 文本
// @return 只读语句为读句柄,其余为写句柄
func (d *DB) route(query string) *sql.DB {
	if isReadOnly(query) {
		return d.read
	}
	return d.write
}

// isReadOnly 判定语句是否只读:跳过前导注释与空白后,首个关键字为 SELECT 或 EXPLAIN。
// @Description: sqlc 生成的语句以 "-- name: X :one" 行注释开头。带 RETURNING 的
// INSERT/UPDATE 首关键字仍是写,不会误判;拿不准的形态(WITH 开头的 CTE 等)按写处理,
// 代价只是多排一次队,不会把改动语句送上读句柄。
// @param query SQL 文本
// @return 只读为 true
func isReadOnly(query string) bool {
	s := query
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if rest, ok := strings.CutPrefix(s, "--"); ok {
			_, after, found := strings.Cut(rest, "\n")
			if !found {
				return false
			}
			s = after
			continue
		}
		if rest, ok := strings.CutPrefix(s, "/*"); ok {
			_, after, found := strings.Cut(rest, "*/")
			if !found {
				return false
			}
			s = after
			continue
		}
		break
	}
	end := 0
	for end < len(s) && (s[end]|0x20 >= 'a' && s[end]|0x20 <= 'z') {
		end++
	}
	switch strings.ToUpper(s[:end]) {
	case "SELECT", "EXPLAIN":
		return true
	}
	return false
}
