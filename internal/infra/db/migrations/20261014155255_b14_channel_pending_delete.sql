-- Add column "pending_delete" to table: "channels"
ALTER TABLE `channels` ADD COLUMN `pending_delete` integer NOT NULL DEFAULT 0;
