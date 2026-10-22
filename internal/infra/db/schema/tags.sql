-- 图片标签:属于用户,名称在用户内唯一;图片与标签的关联见 image_tags。
CREATE TABLE tags (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE, -- 所属用户,标签随图片属主创建
    name TEXT NOT NULL, -- 标签名,去首尾空白后 1 到 32 个字符,区分大小写
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);

CREATE UNIQUE INDEX idx_tags_user_name ON tags (user_id, name);
