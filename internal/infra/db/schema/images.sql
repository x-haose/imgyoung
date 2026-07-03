-- 上传引用:对外短 ID → 物理图,一次上传创建一条,承载 TTL 与访问活跃度。
CREATE TABLE images (
    id TEXT PRIMARY KEY, -- 对外短 ID(nanoid)
    blob_id INTEGER NOT NULL REFERENCES blobs (id), -- 指向的物理图
    user_id TEXT, -- 归属用户,单用户期为 'default'
    file_name TEXT NOT NULL DEFAULT '', -- 原始文件名,仅作元数据展示
    expire_at TEXT, -- 到期时刻,NULL 表示永不过期
    last_public_access_at TEXT, -- 最近一次真实公开访问时刻,NULL 表示从未
    pending_cleanup INTEGER NOT NULL DEFAULT 0, -- 冷图待清理标签,1 表示已入待裁决集
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);

CREATE INDEX idx_images_blob ON images (blob_id);

CREATE INDEX idx_images_expire ON images (expire_at);

CREATE INDEX idx_images_access ON images (last_public_access_at);
