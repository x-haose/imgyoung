package db

import "time"

// DefaultUserID 是迁移播种的默认用户 id(设计定稿·鉴权模型:默认单用户单 Token)。
// 未接入多用户前,所有上传引用归属该用户;迁移里的种子值与此常量必须一致。
// 迭代 5 建号后,本常量随鉴权逻辑迁往 core/auth。
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
