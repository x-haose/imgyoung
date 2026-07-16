-- 上传幂等键:唯一约束即防重入口,按用户隔离防跨用户探测,由 TTL 清理任务定期回收。
CREATE TABLE idempotency_keys (
    user_id TEXT NOT NULL REFERENCES users (id), -- 归属用户,键只在用户内生效
    key TEXT NOT NULL, -- 客户端幂等键
    image_id TEXT NOT NULL REFERENCES images (id), -- 命中时返回的既有图片短 ID
    created_at TEXT NOT NULL DEFAULT (datetime('now')), -- 创建时刻,UTC 文本,保留期清理依据
    PRIMARY KEY (user_id, key)
);
