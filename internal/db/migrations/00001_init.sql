-- +goose Up
-- 迭代 0 最小上传引用表:短 ID → 渠道对象键的映射。
-- 完整数据模型(blobs/images/blob_channels/variants 等)按设计定稿在迭代 1 落表。
CREATE TABLE images (
    -- 对外短 ID(nanoid),同时是主键
    id TEXT PRIMARY KEY,
    -- 渠道内对象键
    storage_key TEXT NOT NULL,
    -- 服务端判定的真实 MIME 类型(绝不信任客户端声明)
    mime TEXT NOT NULL,
    -- 原始字节大小
    size INTEGER NOT NULL,
    -- 上传时间(UTC)
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE images;
