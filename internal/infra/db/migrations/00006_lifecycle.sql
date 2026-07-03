-- +goose Up
-- 生命周期(设计定稿·生命周期与清理):TTL 挂在引用(images)上;
-- 三段式删除的物理删宽限挂在物理图(blobs)上。
ALTER TABLE images ADD COLUMN expire_at TEXT;                              -- 到期时间(UTC);NULL = 永不过期
ALTER TABLE images ADD COLUMN last_public_access_at TEXT;                  -- 最近真实公开访问(活跃度,批量 flush)
ALTER TABLE images ADD COLUMN pending_cleanup INTEGER NOT NULL DEFAULT 0;  -- 冷图待清理标签(人工裁决)
ALTER TABLE blobs ADD COLUMN delete_after TEXT;                            -- 标记待删后允许物理删的时刻;NULL = 未标记

-- 到期扫描与冷图扫描按这些列过滤
CREATE INDEX idx_images_expire ON images (expire_at);
CREATE INDEX idx_images_access ON images (last_public_access_at);
CREATE INDEX idx_blobs_delete_after ON blobs (delete_after);

-- +goose Down
DROP INDEX idx_blobs_delete_after;
DROP INDEX idx_images_access;
DROP INDEX idx_images_expire;
-- 列保留(dev-only 回滚,残留可空列无害),避免旧 SQLite 的 DROP COLUMN 限制。
