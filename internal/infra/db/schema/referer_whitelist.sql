-- 防盗链白名单:公开图片访问的 Referer 规则,后台增删、热生效。
CREATE TABLE referer_whitelist (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    pattern TEXT NOT NULL UNIQUE, -- 规则,小写:裸域(example.com)或子域通配(*.example.com)
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 添加时刻,UTC 文本
);
