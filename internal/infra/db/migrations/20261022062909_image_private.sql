-- Add column "private" to table: "images"
ALTER TABLE `images` ADD COLUMN `private` integer NOT NULL DEFAULT 0;
