-- 图片地址在来源处的引用:同一个地址可以被多个页面或文档、以多种写法引用。
CREATE TABLE import_item_sources (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增),也是发现顺序
    item_id INTEGER NOT NULL REFERENCES import_items (id) ON DELETE CASCADE, -- 被引用的图片地址条目
    source TEXT NOT NULL, -- 引用所在的页面地址或文档名
    raw TEXT NOT NULL -- 引用在该处的原文,可能是相对路径
);

CREATE UNIQUE INDEX idx_import_item_sources_ref ON import_item_sources (item_id, source, raw);
