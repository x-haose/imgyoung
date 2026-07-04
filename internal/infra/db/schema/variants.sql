-- 按需处理的衍生图缓存索引:一物理图 × 一处理参数指纹一行,字节存本地磁盘 LRU;删 blob 时应用层级联清理。
CREATE TABLE variants (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    blob_id INTEGER NOT NULL REFERENCES blobs (id), -- 源物理图
    params_hash TEXT NOT NULL, -- 处理参数指纹(宽度+格式+质量档),兼本地文件名与缓存键
    format TEXT NOT NULL, -- 目标格式 webp/avif/jpeg,命中缓存时据此定 Content-Type
    size INTEGER NOT NULL, -- 衍生图字节大小,LRU 容量核算用
    last_access TEXT NOT NULL DEFAULT (datetime('now')), -- 最近访问时刻,UTC;LRU 淘汰依据
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);

CREATE UNIQUE INDEX idx_variants_key ON variants (blob_id, params_hash);

CREATE INDEX idx_variants_last_access ON variants (last_access);

CREATE INDEX idx_variants_blob ON variants (blob_id);
