-- Add column "access_count" to table: "images"
ALTER TABLE `images` ADD COLUMN `access_count` integer NOT NULL DEFAULT 0;
