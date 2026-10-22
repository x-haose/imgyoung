-- 运营配置:单行宽表(id 恒为 1),本表是运行中配置的唯一真相,后台修改、热生效。
-- 每列 DEFAULT 即出厂默认值,首启插入默认行、升级加配置用 ADD COLUMN 自动补默认。
-- 引导配置(listen / data_dir / master_key / trusted_proxies / metrics_allow)只认环境变量,不入本表。
CREATE TABLE settings (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- 单行守卫:只允许 id=1,永远至多一行
    max_upload_mb INTEGER NOT NULL DEFAULT 50, -- 单图上限(MB)
    max_pixels_mp INTEGER NOT NULL DEFAULT 50, -- 像素预算(百万像素,宽 × 高上限)
    max_concurrent_uploads INTEGER NOT NULL DEFAULT 8, -- 上传并发上限,满载返回 429
    upload_rate_per_min INTEGER NOT NULL DEFAULT 0, -- 每用户每分钟上传次数上限;0=不启用
    base_url TEXT NOT NULL DEFAULT '', -- 对外站点地址(去尾斜杠);空则按请求域名推导
    cdn_cloudflare_zone_id TEXT NOT NULL DEFAULT '', -- Cloudflare Zone ID;三者齐备才启用 CDN purge
    cdn_cloudflare_api_token TEXT NOT NULL DEFAULT '', -- Cloudflare API Token 密文(AES-GCM,base64);空串表示未配置
    allow_empty_referer INTEGER NOT NULL DEFAULT 1, -- 空 Referer 是否放行(布尔 0/1)
    exif_policy TEXT NOT NULL DEFAULT 'passthrough', -- 全局默认 EXIF 档位:passthrough / strip / spoof
    min_success INTEGER NOT NULL DEFAULT 1, -- 上传返回成功所需的最少同步副本数
    access_mode TEXT NOT NULL DEFAULT 'proxy', -- 访问模式:proxy(代理)/ redirect(302 省流量)
    encrypt_new_channels INTEGER NOT NULL DEFAULT 0, -- 新建渠道未显式指定时是否加密(布尔 0/1)
    health_check_seconds INTEGER NOT NULL DEFAULT 60, -- 渠道健康检查间隔(秒)
    backup_interval_hours INTEGER NOT NULL DEFAULT 1, -- 元数据库自动备份间隔(小时)
    backup_keep INTEGER NOT NULL DEFAULT 24, -- 备份保留份数;丢失映射即全部不可反查,宁多勿少
    cleanup_sweep_seconds INTEGER NOT NULL DEFAULT 60, -- 生命周期清理扫描间隔(秒)
    delete_grace_minutes INTEGER NOT NULL DEFAULT 0, -- 引用归零到物理删的宽限(分钟),可为 0
    default_ttl_hours INTEGER NOT NULL DEFAULT 0, -- 全局默认 TTL(小时),0=永不过期
    ttl_renew_on_access INTEGER NOT NULL DEFAULT 1, -- 公开访问是否顺延带 TTL 图片的到期时刻(布尔 0/1)
    activity_flush_seconds INTEGER NOT NULL DEFAULT 60, -- 访问活跃度批量落库间隔(秒)
    cold_cleanup_days INTEGER NOT NULL DEFAULT 90, -- 冷图待清理阈值(天),0=不启用
    usage_report_seconds INTEGER NOT NULL DEFAULT 300, -- 渠道用量上报间隔(秒)
    channel_quota_gb INTEGER NOT NULL DEFAULT 0, -- 单渠道用量告警阈值(GB),0=不告警
    dedup_scan_seconds INTEGER NOT NULL DEFAULT 300, -- 后台查重扫描间隔(秒)
    dedup_phash_max_distance INTEGER NOT NULL DEFAULT 10, -- pHash 相似判定汉明距离上限;0=不做相似扫描
    process_widths TEXT NOT NULL DEFAULT '[100,200,400,800,1600]', -- 允许的宽度档(JSON 数组,像素);[] 表示不允许缩放
    process_formats TEXT NOT NULL DEFAULT '["webp","jpeg","png"]', -- 允许的目标格式(JSON 数组)
    process_quality_webp INTEGER NOT NULL DEFAULT 80, -- 按需处理 webp 质量档[1,100]
    process_quality_jpeg INTEGER NOT NULL DEFAULT 85, -- 按需处理 jpeg 质量档[1,100]
    process_webp_method INTEGER NOT NULL DEFAULT 4, -- webp 质量/速度权衡[0,6]
    process_workers INTEGER NOT NULL DEFAULT 4, -- 处理并发上限(信号量)
    process_timeout_ms INTEGER NOT NULL DEFAULT 5000, -- 单请求处理超时(毫秒)
    variants_cache_mb INTEGER NOT NULL DEFAULT 10240, -- variant 磁盘缓存上限(MB),超则 LRU 淘汰
    idempotency_key_ttl_hours INTEGER NOT NULL DEFAULT 24, -- 上传幂等键保留期(小时),超期由清理循环回收;客户端重试窗口以此为界
    alert_webhook_url TEXT NOT NULL DEFAULT '', -- 告警 Webhook 地址(http/https),告警开启与恢复时 POST JSON;空=不外发
    audit_retention_days INTEGER NOT NULL DEFAULT 0, -- 审计日志保留天数,超期由清理循环回收;0=永久保留
    replicate_limit_mbps INTEGER NOT NULL DEFAULT 0, -- 后台补课写入渠道的总带宽上限(MB/s),所有渠道合计;0=不限;只限补课队列,不限新上传触发的补齐与用户上传的同步写入
    spoof_mode TEXT NOT NULL DEFAULT 'per_image', -- spoof 档的身份模式:per_image(每张图独立随机机型与城市)/ uniform(全站固定 spoof_camera 与 spoof_city)
    spoof_camera TEXT NOT NULL DEFAULT '', -- uniform 模式使用的机型标识;per_image 模式忽略
    spoof_city TEXT NOT NULL DEFAULT '', -- uniform 模式使用的城市标识;per_image 模式忽略
    spoof_countries TEXT NOT NULL DEFAULT '[]', -- per_image 模式候选城市的国家代码(JSON 数组);[] 表示全部
    spoof_window_days INTEGER NOT NULL DEFAULT 90, -- spoof 档的拍摄时间落在上传时刻往前这么多天之内
    max_side_px INTEGER NOT NULL DEFAULT 30000, -- 单边像素上限,超过的图片被拒收
    max_aspect_ratio INTEGER NOT NULL DEFAULT 100, -- 长边与短边之比的上限,超过的图片被拒收
    updated_at TEXT NOT NULL DEFAULT (datetime('now')) -- 最近写入时刻,UTC 文本
);
