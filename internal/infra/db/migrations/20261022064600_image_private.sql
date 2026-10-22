-- Add column "private" to table: "images"
ALTER TABLE `images` ADD COLUMN `private` integer NOT NULL DEFAULT 0;
-- Add column "sign_epoch" to table: "images"
ALTER TABLE `images` ADD COLUMN `sign_epoch` integer NOT NULL DEFAULT 0;
