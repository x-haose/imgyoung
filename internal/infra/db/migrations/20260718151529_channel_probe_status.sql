-- Add column "last_checked_at" to table: "channels"
ALTER TABLE `channels` ADD COLUMN `last_checked_at` text NULL;
-- Add column "last_error" to table: "channels"
ALTER TABLE `channels` ADD COLUMN `last_error` text NULL;
