package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ariga.io/atlas/sql/migrate"
)

// revisionsTable 迁移历史表名;记录每个已应用版本及其执行结果,Executor 据此判定待应用集。
const revisionsTable = "schema_revisions"

// revisions 用业务库自身的一张表实现 Atlas 的 RevisionReadWriter,持久化迁移执行历史。
type revisions struct {
	conn *sql.DB
}

// newRevisions 确保迁移历史表存在并返回读写器。
// @param ctx 控制建表超时与取消
// @param conn 已打开的数据库连接
// @return 就绪的历史读写器
// @return 建表失败时的错误
func newRevisions(ctx context.Context, conn *sql.DB) (*revisions, error) {
	const ddl = `CREATE TABLE IF NOT EXISTS ` + revisionsTable + ` (
	version          TEXT PRIMARY KEY,
	description      TEXT NOT NULL,
	type             INTEGER NOT NULL,
	applied          INTEGER NOT NULL,
	total            INTEGER NOT NULL,
	executed_at      TEXT NOT NULL,
	execution_time   INTEGER NOT NULL,
	error            TEXT NOT NULL DEFAULT '',
	error_stmt       TEXT NOT NULL DEFAULT '',
	hash             TEXT NOT NULL,
	partial_hashes   TEXT NOT NULL DEFAULT '[]',
	operator_version TEXT NOT NULL DEFAULT ''
)`
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		return nil, fmt.Errorf("创建迁移历史表: %w", err)
	}
	return &revisions{conn: conn}, nil
}

// revisionCols 历史表的列清单,读写共用以保证顺序一致。
const revisionCols = `version, description, type, applied, total, executed_at, execution_time, error, error_stmt, hash, partial_hashes, operator_version`

// Ident 返回历史表标识,Executor 的洁净性检查据此排除该表。
// @return 历史表标识
func (r *revisions) Ident() *migrate.TableIdent {
	return &migrate.TableIdent{Name: revisionsTable}
}

// ReadRevisions 返回全部已记录的迁移版本,按版本升序。
// @param ctx 控制查询超时与取消
// @return 已记录的迁移版本
// @return 查询失败时的错误
func (r *revisions) ReadRevisions(ctx context.Context) ([]*migrate.Revision, error) {
	rows, err := r.conn.QueryContext(ctx, `SELECT `+revisionCols+` FROM `+revisionsTable+` ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("读取迁移历史: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*migrate.Revision
	for rows.Next() {
		rev, serr := scanRevision(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// ReadRevision 按版本返回单条记录,不存在时返回 migrate.ErrRevisionNotExist。
// @param ctx 控制查询超时与取消
// @param version 迁移版本
// @return 对应的迁移记录
// @return 不存在返回 ErrRevisionNotExist,其余为查询错误
func (r *revisions) ReadRevision(ctx context.Context, version string) (*migrate.Revision, error) {
	row := r.conn.QueryRowContext(ctx, `SELECT `+revisionCols+` FROM `+revisionsTable+` WHERE version = ?`, version)
	rev, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, migrate.ErrRevisionNotExist
	}
	return rev, err
}

// WriteRevision 幂等写入(存在即更新)一条迁移记录。
// @param ctx 控制写入超时与取消
// @param rev 待持久化的迁移记录
// @return 写入失败时的错误
func (r *revisions) WriteRevision(ctx context.Context, rev *migrate.Revision) error {
	hashes, err := json.Marshal(rev.PartialHashes)
	if err != nil {
		return fmt.Errorf("编码分段哈希: %w", err)
	}
	_, err = r.conn.ExecContext(ctx, `INSERT INTO `+revisionsTable+` (`+revisionCols+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (version) DO UPDATE SET
	description=excluded.description, type=excluded.type, applied=excluded.applied, total=excluded.total,
	executed_at=excluded.executed_at, execution_time=excluded.execution_time, error=excluded.error,
	error_stmt=excluded.error_stmt, hash=excluded.hash, partial_hashes=excluded.partial_hashes,
	operator_version=excluded.operator_version`,
		rev.Version, rev.Description, uint(rev.Type), rev.Applied, rev.Total,
		rev.ExecutedAt.UTC().Format(time.RFC3339Nano), int64(rev.ExecutionTime),
		rev.Error, rev.ErrorStmt, rev.Hash, string(hashes), rev.OperatorVersion)
	if err != nil {
		return fmt.Errorf("写入迁移记录 %s: %w", rev.Version, err)
	}
	return nil
}

// DeleteRevision 删除指定版本的记录。
// @param ctx 控制删除超时与取消
// @param version 迁移版本
// @return 删除失败时的错误
func (r *revisions) DeleteRevision(ctx context.Context, version string) error {
	if _, err := r.conn.ExecContext(ctx, `DELETE FROM `+revisionsTable+` WHERE version = ?`, version); err != nil {
		return fmt.Errorf("删除迁移记录 %s: %w", version, err)
	}
	return nil
}

// rowScanner 兼容 *sql.Row 与 *sql.Rows 的 Scan。
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRevision 把一行历史记录还原为 migrate.Revision。
// @param s 单行或多行结果的扫描器
// @return 还原后的迁移记录
// @return 扫描或解码失败时的错误
func scanRevision(s rowScanner) (*migrate.Revision, error) {
	var (
		rev       migrate.Revision
		typ       uint
		executed  string
		execNanos int64
		hashes    string
	)
	if err := s.Scan(&rev.Version, &rev.Description, &typ, &rev.Applied, &rev.Total,
		&executed, &execNanos, &rev.Error, &rev.ErrorStmt, &rev.Hash, &hashes, &rev.OperatorVersion); err != nil {
		return nil, err
	}
	rev.Type = migrate.RevisionType(typ)
	rev.ExecutionTime = time.Duration(execNanos)
	parsed, perr := time.Parse(time.RFC3339Nano, executed)
	if perr != nil {
		return nil, fmt.Errorf("解析迁移执行时刻 %q: %w", executed, perr)
	}
	rev.ExecutedAt = parsed
	if err := json.Unmarshal([]byte(hashes), &rev.PartialHashes); err != nil {
		return nil, fmt.Errorf("解码分段哈希: %w", err)
	}
	return &rev, nil
}
