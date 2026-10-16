package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
)

// ErrCorrupt 文件不是完整的 SQLite 数据库(被截断、内容损坏或根本不是数据库文件)。
var ErrCorrupt = errors.New("db: 不是完整的 SQLite 数据库")

// 判定文件损坏的 SQLite 主错误码。
const (
	sqliteCorrupt = 11 // SQLITE_CORRUPT
	sqliteNotADB  = 26 // SQLITE_NOTADB
)

// CheckSnapshot 以只读方式打开一个元数据库快照,核对它是完整的 SQLite 数据库且记有迁移历史,不改动文件。
// @Description: 用于换入前核对快照:换入残缺的库,进程开库或迁移时才失败,实例就起不来;换入 0 字节文件(SQLite
// 视为合法的空库),迁移会把它建成一个全新实例,原有数据全部看不见。只有 SQLite 报文件损坏、不是数据库,完整性
// 检查列出问题,或库里没有迁移历史时才判为不完整;读盘出错、内存不足这类本机故障如实返回,不归咎于文件。
// @param ctx 控制超时与取消;检查要读全部页面,大库需要数秒
// @param path 待检查的文件路径
// @return 不完整时包裹 ErrCorrupt 的错误;ctx 结束时为 ctx 的错误;其余为打开或读取失败的错误
func CheckSnapshot(ctx context.Context, path string) error {
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("打开 %s: %w", path, err)
	}
	defer func() { _ = conn.Close() }()
	if err := quickCheck(ctx, conn, path); err != nil {
		return err
	}
	var revisions int64
	err = conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, revisionsTable).Scan(&revisions)
	if err != nil {
		return fmt.Errorf("检查 %s 的迁移历史: %w", path, err)
	}
	if revisions == 0 {
		return fmt.Errorf("%w: 没有迁移历史,不是元数据库", ErrCorrupt)
	}
	return nil
}

// quickCheck 对已打开的库执行 PRAGMA quick_check。
// @param conn 只读连接
// @param path 文件路径,只用于错误信息
// @return 不完整时包裹 ErrCorrupt 的错误;ctx 结束时为 ctx 的错误;其余为读取失败的错误
func quickCheck(ctx context.Context, conn *sql.DB, path string) error {
	var result string
	err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	switch {
	case isCorruption(err):
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	case err != nil:
		return fmt.Errorf("检查 %s: %w", path, err)
	case result != "ok":
		return fmt.Errorf("%w: %s", ErrCorrupt, result)
	}
	return nil
}

// isCorruption 判定错误是否为 SQLite 报告的文件损坏或不是数据库。
// @param err 待判定错误
// @return 命中为 true
func isCorruption(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	primary := se.Code() & 0xff
	return primary == sqliteCorrupt || primary == sqliteNotADB
}
