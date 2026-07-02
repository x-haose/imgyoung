-- +goose Up
-- 渠道与副本模型(见设计定稿·数据模型/渠道管理)。
CREATE TABLE channels (
    -- 内部主键
    id INTEGER PRIMARY KEY,
    -- 渠道名(唯一,展示与日志用)
    name TEXT NOT NULL UNIQUE,
    -- rclone 后端名(local/s3/webdav 等)
    driver TEXT NOT NULL,
    -- 加密后的参数 JSON(secret.EncryptString 产物);空串表示无参数
    config_sealed TEXT NOT NULL DEFAULT '',
    -- 优先级,越大越优先;"主渠道"即健康渠道中的最高优先级(设计定稿)
    priority INTEGER NOT NULL DEFAULT 100,
    -- 健康标记,由健康检查维护,参与渠道筛选
    healthy INTEGER NOT NULL DEFAULT 1,
    -- 存储加密(crypt 包裹):0/1,创建渠道时解析全局默认后固化,
    -- 建后不可变——变更会使该渠道既有对象不可读
    encrypted INTEGER NOT NULL DEFAULT 0,
    -- 是否允许 302 直链(密文渠道与本地渠道应为 0)
    allow_direct_link INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 物理图 × 渠道副本状态,兼异步补齐的任务状态(outbox,业务表是唯一真相)
CREATE TABLE blob_channels (
    blob_id INTEGER NOT NULL REFERENCES blobs (id),
    channel_id INTEGER NOT NULL REFERENCES channels (id),
    -- 副本状态机:pending → uploading → done / failed
    state TEXT NOT NULL DEFAULT 'pending',
    -- 累计尝试次数
    attempts INTEGER NOT NULL DEFAULT 0,
    -- 最近一次失败原因(展示与排查用)
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (blob_id, channel_id)
);
CREATE INDEX idx_blob_channels_state ON blob_channels (state);
CREATE INDEX idx_blob_channels_channel ON blob_channels (channel_id);

-- 内置默认 local 渠道,并把既有 blob 回填为其已完成副本(平滑升级)。
INSERT INTO channels (name, driver, priority)
VALUES ('local', 'local', 100);

INSERT INTO blob_channels (blob_id, channel_id, state)
SELECT b.id, (SELECT id FROM channels WHERE name = 'local'), 'done'
FROM blobs b;

-- +goose Down
DROP TABLE blob_channels;
DROP TABLE channels;
