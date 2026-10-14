-- 物理图:去重与副本的最小单位,一张物理图可被多个 images 引用。
CREATE TABLE blobs (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    sha256 TEXT NOT NULL, -- 净化后字节的 SHA256(十六进制),哈希对象即实际存储字节
    phash INTEGER NOT NULL, -- 感知哈希(64 位按 int64 存储),供后台相似度查重
    storage_key TEXT NOT NULL, -- 渠道内对象键,形如 {short_id}_{sha256 前 8 位}
    mime TEXT NOT NULL, -- 服务端判定的真实 MIME
    size INTEGER NOT NULL, -- 存储字节大小
    width INTEGER NOT NULL, -- 像素宽
    height INTEGER NOT NULL, -- 像素高
    ref_count INTEGER NOT NULL DEFAULT 1, -- 引用计数,归零走三段式删除
    pending_delete INTEGER NOT NULL DEFAULT 0, -- 待物理删标记,1 表示处于宽限期
    delete_after TEXT, -- 物理删执行时刻,NULL 表示未进入删除流程
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);

CREATE INDEX idx_blobs_sha256 ON blobs (sha256);

CREATE INDEX idx_blobs_delete_after ON blobs (delete_after);
