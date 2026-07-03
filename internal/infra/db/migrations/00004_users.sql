-- +goose Up
-- 多租户地基(设计定稿·鉴权模型):users 表 + 默认用户;images.user_id 从此写入。
-- 运行时默认单用户('default'),迭代 5 上多用户建号。images.user_id 现为可空 TEXT
-- (00002 已预留),本迁移建 users、播种默认用户、回填既有引用;列的 NOT NULL/FK 收紧
-- 留到迭代 5 真实用户落地时随表重建——自部署量级,届时重建成本可忽略。
CREATE TABLE users (
    -- 用户 id;真实用户用 nanoid,默认用户保留固定 'default'
    id TEXT PRIMARY KEY,
    -- 登录名/展示名,唯一
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO users (id, name) VALUES ('default', 'default');

-- 既有引用归入默认用户,避免遗留 NULL。
UPDATE images SET user_id = 'default' WHERE user_id IS NULL;

-- +goose Down
DROP TABLE users;
