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
    created_at TEXT NOT NULL DEFAULT (datetime('now')), -- 创建时刻,UTC 文本;参与加密渠道的密钥派生,建后不变
    bound_at TEXT NOT NULL DEFAULT (datetime('now')), -- 绑定时刻,UTC 文本:本记录开始对应当前存储位置的时刻,新建时与 created_at 同值,编辑改动配置或换入恢复快照时重置;对账 GC 只回收修改时间不早于它的孤儿
    last_checked_at TEXT, -- 最近一次健康探测时刻,UTC 文本;NULL 表示尚未探测过
    last_error_stage TEXT, -- 最近一次失败的环节:probe 探测 / load 装载驱动 / drain 清理对象;NULL 表示最近一次探测成功,三个 last_error 列同为 NULL 或同不为 NULL
    last_error_reason TEXT, -- 最近一次失败的归类,取值即接口契约的 FailureReason 枚举;NULL 同 last_error_stage
    last_error TEXT, -- 最近一次失败的根因原文(已抹除凭证、截断),供排障;NULL 同 last_error_stage
    template TEXT NOT NULL DEFAULT '', -- 配置所用模板 id,空串表示自定义配置;仅供编辑表单还原,不参与驱动构造
    pending_delete INTEGER NOT NULL DEFAULT 0 -- 清理中标记,1 表示正在删除渠道上的对象、清完即删行;期间不参与选路、补齐与探测
);
