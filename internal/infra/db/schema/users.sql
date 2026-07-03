-- 用户:单用户期只有固定的 'default' 一行,多用户体系落地后承载真实用户。
CREATE TABLE users (
    id TEXT PRIMARY KEY, -- 真实用户用 nanoid,默认用户保留固定 'default'
    name TEXT NOT NULL UNIQUE, -- 登录名/展示名
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);
