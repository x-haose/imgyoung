-- 用户:'default' 行是内置管理员,多用户由管理员在后台建号,不开放注册。
CREATE TABLE users (
    id TEXT PRIMARY KEY, -- 真实用户用 nanoid,内置管理员保留固定 'default'
    name TEXT NOT NULL UNIQUE, -- 登录名/展示名
    password_hash TEXT NOT NULL DEFAULT '', -- argon2id PHC 串;空串表示未设密码、禁止登录
    role TEXT NOT NULL DEFAULT 'user', -- 角色:admin 管理全站,user 只见自己的资源
    quota_bytes INTEGER, -- 存储配额上限,按引用计全额;NULL 表示不限
    disabled INTEGER NOT NULL DEFAULT 0, -- 禁用标记,1 表示禁止登录与上传
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);
