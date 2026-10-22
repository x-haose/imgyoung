-- 批量导入任务:一次导入的输入类型、参数、状态与扫描进度;逐条的图片地址见 import_items。
CREATE TABLE import_jobs (
    id TEXT PRIMARY KEY, -- 对外任务 ID(nanoid)
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE, -- 归属用户,导入的图片归属于他
    mode TEXT NOT NULL, -- 输入类型:images / text / site
    status TEXT NOT NULL, -- 任务状态:creating(条目分批写入中,对外不可见) / scanning / ready / importing / done / cancelled / aborted
    seeds TEXT NOT NULL DEFAULT '[]', -- site 任务的起始地址,JSON 字符串数组;其他类型为 []
    ttl_seconds INTEGER, -- 导入图片的有效期,秒;0 表示永久,NULL 表示按用户默认与全局默认
    exif_policy TEXT NOT NULL DEFAULT '', -- 导入图片的 EXIF 档位 strip / passthrough;空串表示全局默认
    referer TEXT NOT NULL DEFAULT '', -- 抓取图片时发送的 Referer;空串表示取条目的第一个来源页
    scan_pages_found INTEGER NOT NULL DEFAULT 0, -- 扫描中已发现的页面数
    scan_pages_done INTEGER NOT NULL DEFAULT 0, -- 扫描中已处理的页面数
    scan_report TEXT, -- 扫描结束时的汇总,JSON 对象;NULL 表示没有扫描过程或尚未扫描完
    error_code TEXT NOT NULL DEFAULT '', -- aborted 的原因码;其他状态为空串
    created_at TEXT NOT NULL DEFAULT (datetime('now')), -- 创建时刻,UTC 文本
    finished_at TEXT -- 进入 done / cancelled / aborted 的时刻,UTC 文本;NULL 表示未结束
);

CREATE INDEX idx_import_jobs_user ON import_jobs (user_id, created_at);

CREATE INDEX idx_import_jobs_status ON import_jobs (status, created_at);
