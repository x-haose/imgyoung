-- +goose Up
-- 迭代 1:落地设计定稿的两层核心模型(物理图 blobs / 上传引用 images)。
-- 上传永远新建 blob(sha256 不唯一),精确/相似合并由后台任务处理(迭代 6)。
DROP TABLE images;

CREATE TABLE blobs (
    -- 内部主键(自增)
    id INTEGER PRIMARY KEY,
    -- 净化后字节的 SHA256(十六进制);哈希对象 = 实际存储字节
    sha256 TEXT NOT NULL,
    -- 感知哈希(64 位,按 int64 存储),供后台相似度查重
    phash INTEGER NOT NULL,
    -- MD5,可空:仅需要 ETag 校验的渠道才计算(迭代 2+)
    md5 TEXT,
    -- 渠道内对象键,形如 {short_id}_{sha256 前 8 位}
    storage_key TEXT NOT NULL,
    -- 服务端判定的真实 MIME
    mime TEXT NOT NULL,
    -- 存储字节大小
    size INTEGER NOT NULL,
    -- 像素尺寸
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    -- 引用计数(计数列,原子增减;归零走三段式删除)
    ref_count INTEGER NOT NULL DEFAULT 1,
    -- 删除标记(1 = 待物理删,处于宽限期)
    pending_delete INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
-- 后台精确查重按 sha256 分组;非唯一索引
CREATE INDEX idx_blobs_sha256 ON blobs (sha256);

CREATE TABLE images (
    -- 对外短 ID(nanoid)
    id TEXT PRIMARY KEY,
    blob_id INTEGER NOT NULL REFERENCES blobs (id),
    -- 预留:多用户体系(迭代 5)
    user_id TEXT,
    -- 原始文件名,仅作元数据展示
    file_name TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_images_blob ON images (blob_id);

CREATE TABLE idempotency_keys (
    -- 客户端幂等键(唯一约束即防重入口)
    key TEXT PRIMARY KEY,
    image_id TEXT NOT NULL REFERENCES images (id),
    -- 保留期清理依据(迭代 4 起随 TTL sweeper 一起回收)
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE images;
DROP TABLE blobs;
CREATE TABLE images (
    id TEXT PRIMARY KEY,
    storage_key TEXT NOT NULL,
    mime TEXT NOT NULL,
    size INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
