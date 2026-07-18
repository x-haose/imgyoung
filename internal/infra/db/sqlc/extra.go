// Package sqlc 提供 sqlc 生成的类型安全查询层,及与之同包的手写辅助。
// db.go/models.go/queries.sql.go 由 sqlc 生成,勿手改;本文件为手写扩展。
package sqlc

import (
	"database/sql"
	"fmt"
	"time"
)

// DefaultUserID 是迁移播种的默认用户 id(默认单用户单 Token)。
// 未接入多用户前,所有上传引用归属该用户;迁移里的种子值与此常量必须一致。
const DefaultUserID = "default"

// timeLayout 与 SQLite datetime('now') 一致的文本时间格式(UTC、空格分隔)。
// 用它写入 expire_at / delete_after,保证与 created_at 等列可做字典序即时间序比较。
const timeLayout = "2006-01-02 15:04:05"

// TimeText 把时刻格式化为 SQLite 可比较的 UTC 文本时间。
// @param t 待格式化的时刻
// @return "YYYY-MM-DD HH:MM:SS" 形式的 UTC 文本
func TimeText(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// TimeNull 把时刻包装为非空的可空文本时间列值(可空列的写入与比较参数形态)。
// @param t 待格式化的时刻
// @return Valid 恒为 true 的可空文本时间
func TimeNull(t time.Time) sql.NullString {
	return sql.NullString{String: TimeText(t), Valid: true}
}

// ParseTimeText 把 TimeText 形态的 UTC 文本时间解析回时刻。
// @param s "YYYY-MM-DD HH:MM:SS" 形式的 UTC 文本
// @return 解析出的 UTC 时刻
// @return 文本不合形态时的错误
func ParseTimeText(s string) (time.Time, error) {
	t, err := time.ParseInLocation(timeLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析文本时间 %q: %w", s, err)
	}
	return t, nil
}
