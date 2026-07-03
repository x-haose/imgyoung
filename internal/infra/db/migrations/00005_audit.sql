-- +goose Up
-- 管理操作审计(设计定稿·可观测性/审计):记录渠道增删、合并、Token 事件等。
-- 后台可查(迭代 5),此处先建表并从渠道增删开始留痕。
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY,
    -- 操作者 user id(单用户期为 'default')
    actor TEXT NOT NULL,
    -- 动作标识,形如 channel.create / channel.delete
    action TEXT NOT NULL,
    -- 操作目标标识(渠道名/id 等),可空
    target TEXT NOT NULL DEFAULT '',
    -- 附加说明,可空
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
-- 后台按时间倒序翻页查看
CREATE INDEX idx_audit_created ON audit_log (created_at);

-- +goose Down
DROP TABLE audit_log;
