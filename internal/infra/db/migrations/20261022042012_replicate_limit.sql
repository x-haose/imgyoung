-- Add column "replicate_limit_mbps" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `replicate_limit_mbps` integer NOT NULL DEFAULT 0;
