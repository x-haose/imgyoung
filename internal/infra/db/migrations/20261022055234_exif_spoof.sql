-- Add column "spoof_mode" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `spoof_mode` text NOT NULL DEFAULT 'per_image';
-- Add column "spoof_camera" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `spoof_camera` text NOT NULL DEFAULT '';
-- Add column "spoof_city" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `spoof_city` text NOT NULL DEFAULT '';
-- Add column "spoof_countries" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `spoof_countries` text NOT NULL DEFAULT '[]';
-- Add column "spoof_window_days" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `spoof_window_days` integer NOT NULL DEFAULT 90;
-- Add column "max_side_px" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `max_side_px` integer NOT NULL DEFAULT 30000;
-- Add column "max_aspect_ratio" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `max_aspect_ratio` integer NOT NULL DEFAULT 100;
