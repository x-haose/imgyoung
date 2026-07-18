-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_images" table
CREATE TABLE `new_images` (
  `id` text NULL,
  `blob_id` integer NOT NULL,
  `user_id` text NOT NULL,
  `token_id` integer NULL,
  `file_name` text NOT NULL DEFAULT '',
  `expire_at` text NULL,
  `ttl_seconds` integer NULL,
  `last_public_access_at` text NULL,
  `pending_cleanup` integer NOT NULL DEFAULT 0,
  `cleanup_exempt` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`token_id`) REFERENCES `tokens` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `2` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Copy rows from old table "images" to new temporary table "new_images"
INSERT INTO `new_images` (`id`, `blob_id`, `user_id`, `token_id`, `file_name`, `expire_at`, `ttl_seconds`, `last_public_access_at`, `pending_cleanup`, `cleanup_exempt`, `created_at`) SELECT `id`, `blob_id`, `user_id`, `token_id`, `file_name`, `expire_at`, `ttl_seconds`, `last_public_access_at`, `pending_cleanup`, `cleanup_exempt`, `created_at` FROM `images`;
-- Drop "images" table after copying rows
DROP TABLE `images`;
-- Rename temporary table "new_images" to "images"
ALTER TABLE `new_images` RENAME TO `images`;
-- Create index "idx_images_blob" to table: "images"
CREATE INDEX `idx_images_blob` ON `images` (`blob_id`);
-- Create index "idx_images_user" to table: "images"
CREATE INDEX `idx_images_user` ON `images` (`user_id`);
-- Create index "idx_images_token" to table: "images"
CREATE INDEX `idx_images_token` ON `images` (`token_id`);
-- Create index "idx_images_expire" to table: "images"
CREATE INDEX `idx_images_expire` ON `images` (`expire_at`);
-- Create index "idx_images_access" to table: "images"
CREATE INDEX `idx_images_access` ON `images` (`last_public_access_at`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
