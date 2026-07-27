-- 上传引用:对外短 ID → 物理图,一次上传创建一条,承载 TTL 与访问活跃度。
CREATE TABLE images (
    id TEXT PRIMARY KEY, -- 对外短 ID(nanoid)
    blob_id INTEGER NOT NULL REFERENCES blobs (id), -- 指向的物理图
    user_id TEXT NOT NULL REFERENCES users (id), -- 归属用户,单用户期为 'default'
    token_id INTEGER REFERENCES tokens (id) ON DELETE SET NULL, -- 经哪个 Token 上传,NULL 表示非 Token 途径或来源 Token 记录已删除
    file_name TEXT NOT NULL DEFAULT '', -- 原始文件名,仅作元数据展示
    object_key TEXT, -- S3 协议上传时客户端指定的对象键,可经 /o/<用户 id>/<键> 访问;NULL 表示非 S3 途径
    expire_at TEXT, -- 到期时刻,NULL 表示永不过期
    ttl_seconds INTEGER, -- 上传时确定的有效时长,秒;访问续期按此顺延 expire_at;NULL 表示永久或未设置(不参与续期)
    last_public_access_at TEXT, -- 最近一次真实公开访问时刻,NULL 表示从未
    access_count INTEGER NOT NULL DEFAULT 0, -- 累计公开访问次数;计数口径为活跃度 flush 周期粒度(同周期内多次访问计 1),非精确 PV
    pending_cleanup INTEGER NOT NULL DEFAULT 0, -- 冷图待清理标签,1 表示已入待裁决集
    cleanup_exempt INTEGER NOT NULL DEFAULT 0, -- 清理豁免,1 表示裁决保留、不再参与冷图打标
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);

CREATE INDEX idx_images_blob ON images (blob_id);

-- 同一用户内对象键唯一:S3 语义是"同键覆盖",唯一约束把覆盖变成可检测的冲突,由上传逻辑改指新图。
CREATE UNIQUE INDEX idx_images_object_key ON images (user_id, object_key) WHERE object_key IS NOT NULL;

CREATE INDEX idx_images_user ON images (user_id);

CREATE INDEX idx_images_token ON images (token_id);

CREATE INDEX idx_images_expire ON images (expire_at);

CREATE INDEX idx_images_access ON images (last_public_access_at);
