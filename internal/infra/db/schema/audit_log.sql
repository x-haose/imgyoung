-- 管理操作留痕:记录渠道增删等敏感动作,只追加不修改。
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    actor TEXT NOT NULL, -- 操作者 user id,单用户期为 'default'
    action TEXT NOT NULL, -- 动作标识,形如 channel.create / channel.delete
    target TEXT NOT NULL DEFAULT '', -- 操作目标标识(渠道名/id 等),可空
    detail TEXT NOT NULL DEFAULT '', -- 附加说明,可空
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 记录时刻,UTC 文本,按此检索
);

CREATE INDEX idx_audit_created ON audit_log (created_at);
