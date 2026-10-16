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

// QuickCheck 以只读方式打开一个 SQLite 文件并做快速完整性检查,不改动文件。
// @Description: 用于换入前核对快照:换入残缺的库,进程开库或迁移时才失败,实例就起不来。只有 SQLite 报文件损坏、
// 不是数据库,或检查本身列出问题时才判为不完整;读盘出错、内存不足这类本机故障如实返回,不归咎于文件。
// @param ctx 控制超时与取消;检查要读全部页面,大库需要数秒
// @param path 待检查的文件路径
// @return 不完整时包裹 ErrCorrupt 的错误;ctx 结束时为 ctx 的错误;其余为打开或读取失败的错误
func QuickCheck(ctx context.Context, path string) error {
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("打开 %s: %w", path, err)
	}
	defer func() { _ = conn.Close() }()
	var result string
	err = conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result)
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
