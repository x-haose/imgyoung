-- Add column "last_error_reason" to table: "blob_channels"
ALTER TABLE `blob_channels` ADD COLUMN `last_error_reason` text NOT NULL DEFAULT '';
-- Add column "params" to table: "alerts"
ALTER TABLE `alerts` ADD COLUMN `params` text NULL;
-- Add column "last_error_stage" to table: "channels"
ALTER TABLE `channels` ADD COLUMN `last_error_stage` text NULL;
-- Add column "last_error_reason" to table: "channels"
ALTER TABLE `channels` ADD COLUMN `last_error_reason` text NULL;
-- Backfill: 存量失败原因是中文整句,无法还原归类,记为 unclassified,原句留作原文;下一次探测或补齐即覆盖
UPDATE `channels` SET `last_error_stage` = CASE WHEN `pending_delete` = 1 THEN 'drain' WHEN `last_error` LIKE '装载失败%' THEN 'load' ELSE 'probe' END, `last_error_reason` = 'unclassified' WHERE `last_error` IS NOT NULL;
UPDATE `blob_channels` SET `last_error_reason` = 'unclassified' WHERE `last_error` <> '';
