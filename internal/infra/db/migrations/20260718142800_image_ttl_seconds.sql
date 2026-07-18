-- Add column "ttl_seconds" to table: "images"
ALTER TABLE `images` ADD COLUMN `ttl_seconds` integer NULL;
-- Backfill: 存量带 TTL 的行按 (expire_at - created_at) 推导原始时长(秒)。
UPDATE `images`
SET `ttl_seconds` = CAST(ROUND((julianday(`expire_at`) - julianday(`created_at`)) * 86400) AS INTEGER)
WHERE `expire_at` IS NOT NULL;
