-- 相似候选:pHash 扫描发现的疑似重复对,人工裁决的数据源;约定 blob_a < blob_b,同对唯一。
CREATE TABLE dedup_candidates (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    blob_a INTEGER NOT NULL REFERENCES blobs (id), -- 候选对中 id 较小的物理图
    blob_b INTEGER NOT NULL REFERENCES blobs (id), -- 候选对中 id 较大的物理图
    distance INTEGER NOT NULL, -- pHash 汉明距离,越小越相似
    status TEXT NOT NULL DEFAULT 'pending', -- 裁决状态:pending 待裁决 / merged 已合并 / dismissed 已忽略
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 发现时刻,UTC 文本
);

CREATE UNIQUE INDEX idx_dedup_candidates_pair ON dedup_candidates (blob_a, blob_b);

CREATE INDEX idx_dedup_candidates_status ON dedup_candidates (status);
