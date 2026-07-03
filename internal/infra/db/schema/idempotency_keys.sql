-- 上传幂等键:唯一约束即防重入口,由 TTL 清理任务定期回收。
CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY, -- 客户端幂等键
    image_id TEXT NOT NULL REFERENCES images (id), -- 命中时返回的既有图片短 ID
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本,保留期清理依据
);
