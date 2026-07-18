-- Add column "template" to table: "channels"
ALTER TABLE `channels` ADD COLUMN `template` text NOT NULL DEFAULT '';
