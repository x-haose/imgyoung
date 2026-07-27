-- Create index "idx_tokens_prefix" to table: "tokens"
CREATE UNIQUE INDEX `idx_tokens_prefix` ON `tokens` (`prefix`);
-- Add column "object_key" to table: "images"
ALTER TABLE `images` ADD COLUMN `object_key` text NULL;
-- Create index "idx_images_object_key" to table: "images"
CREATE UNIQUE INDEX `idx_images_object_key` ON `images` (`user_id`, `object_key`) WHERE object_key IS NOT NULL;
