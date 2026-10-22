-- 导入任务里的一个图片地址:扫描发现或用户直接给出,按状态推进到已导入或失败。
CREATE TABLE import_items (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增),也是发现顺序
    job_id TEXT NOT NULL REFERENCES import_jobs (id) ON DELETE CASCADE, -- 所属任务
    url TEXT NOT NULL, -- 图片的绝对地址,已规范化(去掉 # 之后的部分,协议与域名小写),任务内唯一
    host TEXT NOT NULL, -- url 的域名,小写,非默认端口时带端口;确认页按它分组
    state TEXT NOT NULL, -- 条目状态:candidate / pending / done / failed / skipped
    image_id TEXT REFERENCES images (id) ON DELETE SET NULL, -- 导入成功后的图片短 ID;图片被删除后置空
    error_code TEXT NOT NULL DEFAULT '', -- failed 的原因码,取值同上传错误码;其他状态为空串
    error_detail TEXT NOT NULL DEFAULT '' -- failed 的根因原文,最长 500 个字符;其他状态为空串
);

CREATE UNIQUE INDEX idx_import_items_job_url ON import_items (job_id, url);

CREATE INDEX idx_import_items_job_state ON import_items (job_id, state, id);

CREATE INDEX idx_import_items_job_host ON import_items (job_id, host, state);

CREATE INDEX idx_import_items_image ON import_items (image_id);
