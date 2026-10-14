package db

import (
	"errors"
	"strings"

	"modernc.org/sqlite"
)

// modernc 驱动的扩展错误码;驱动不暴露类型化的约束错误,只能按码判定。
const (
	sqliteConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
	sqliteConstraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
)

// IsUniqueViolation 判定错误是否为指定列的唯一约束冲突。
// @param err 待判定错误
// @param column 形如 "users.name" 的约束列
// @return 命中为 true
func IsUniqueViolation(err error, column string) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteConstraintUnique && strings.Contains(se.Error(), column)
}

// IsForeignKeyViolation 判定错误是否为外键约束冲突。
// @param err 待判定错误
// @return 命中为 true
func IsForeignKeyViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteConstraintForeignKey
}
