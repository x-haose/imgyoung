package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrCorrupt 文件不是完整的 SQLite 数据库(被截断、内容损坏或根本不是数据库文件)。
var ErrCorrupt = errors.New("db: 不是完整的 SQLite 数据库")

// QuickCheck 以只读方式打开一个 SQLite 文件并做快速完整性检查,不改动文件。
// @Description: 用于换入前核对快照:换入残缺的库,进程开库或迁移时才失败,实例就起不来。
// @param ctx 控制超时与取消;检查要读全部页面,大库需要数秒
// @param path 待检查的文件路径
// @return 不完整时包裹 ErrCorrupt 的错误;ctx 结束时为 ctx 的错误
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
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if result != "ok" {
		return fmt.Errorf("%w: %s", ErrCorrupt, result)
	}
	return nil
}
