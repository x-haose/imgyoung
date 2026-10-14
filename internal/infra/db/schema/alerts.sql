-- 告警:评估循环按现状开启与恢复,同一(kind, target)至多一条未恢复;Webhook 投递是否成功随行记录。
CREATE TABLE alerts (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    kind TEXT NOT NULL, -- 告警类型:channel_unhealthy / replication_failed / channel_quota_exceeded / drain_failed / backup_failed
    target TEXT NOT NULL DEFAULT '', -- 告警对象(渠道名等);无对象时为空串
    message TEXT NOT NULL, -- 最近一次评估得到的人话描述
    opened_at TEXT NOT NULL DEFAULT (datetime('now')), -- 开启时刻,UTC 文本
    resolved_at TEXT, -- 恢复时刻,UTC 文本;NULL 表示仍未恢复
    open_notified INTEGER NOT NULL DEFAULT 0, -- 开启是否已投递 Webhook(0/1);未投递的下一轮重试
    resolve_notified INTEGER NOT NULL DEFAULT 0 -- 恢复是否已投递 Webhook(0/1);未投递的下一轮重试
);

CREATE UNIQUE INDEX idx_alerts_open ON alerts (kind, target) WHERE resolved_at IS NULL;

CREATE INDEX idx_alerts_opened_at ON alerts (opened_at);
