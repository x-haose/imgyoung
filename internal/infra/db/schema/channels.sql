-- 存储渠道:rclone 后端的一份具名配置,参与上传选路与访问容灾。
CREATE TABLE channels (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    name TEXT NOT NULL UNIQUE, -- 渠道名,展示与日志用
    driver TEXT NOT NULL, -- rclone 后端名(local/s3/webdav 等)
    config_sealed TEXT NOT NULL DEFAULT '', -- 加密后的参数 JSON(secret.EncryptString 产物),空串表示无参数
    priority INTEGER NOT NULL DEFAULT 100, -- 优先级,越大越优先;主渠道即健康渠道中的最高优先级
    healthy INTEGER NOT NULL DEFAULT 1, -- 健康标记,由健康检查维护,参与渠道筛选
    encrypted INTEGER NOT NULL DEFAULT 0, -- 存储加密(crypt 包裹),创建时固化,建后不可变
    allow_direct_link INTEGER NOT NULL DEFAULT 0, -- 是否允许 302 直链,密文渠道与本地渠道应为 0
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 创建时刻,UTC 文本
);
