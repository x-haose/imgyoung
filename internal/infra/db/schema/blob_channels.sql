-- 物理图 × 渠道的副本状态,兼异步补齐的任务状态(outbox,业务表是唯一真相)。
CREATE TABLE blob_channels (
    blob_id INTEGER NOT NULL REFERENCES blobs (id), -- 物理图
    channel_id INTEGER NOT NULL REFERENCES channels (id), -- 渠道
    state TEXT NOT NULL DEFAULT 'pending', -- 副本状态机:pending → uploading → done / failed
    attempts INTEGER NOT NULL DEFAULT 0, -- 累计尝试次数
    last_error_reason TEXT NOT NULL DEFAULT '', -- 最近一次失败的归类,取值即接口契约的 FailureReason 枚举;空串表示没有失败
    last_error TEXT NOT NULL DEFAULT '', -- 最近一次失败的根因原文(已抹除凭证、截断),供排障;空串表示没有失败
    updated_at TEXT NOT NULL DEFAULT (datetime('now')), -- 状态最近更新时刻,UTC 文本
    PRIMARY KEY (blob_id, channel_id) -- 一图一渠道一行
);

CREATE INDEX idx_blob_channels_state ON blob_channels (state);

CREATE INDEX idx_blob_channels_channel ON blob_channels (channel_id);
