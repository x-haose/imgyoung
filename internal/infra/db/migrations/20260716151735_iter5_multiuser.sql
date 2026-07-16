-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_idempotency_keys" table
CREATE TABLE `new_idempotency_keys` (
  `user_id` text NOT NULL,
  `key` text NOT NULL,
  `image_id` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`user_id`, `key`),
  CONSTRAINT `0` FOREIGN KEY (`image_id`) REFERENCES `images` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Copy rows from old table "idempotency_keys" to new temporary table "new_idempotency_keys"
INSERT INTO `new_idempotency_keys` (`user_id`, `key`, `image_id`, `created_at`) SELECT 'default', `key`, `image_id`, `created_at` FROM `idempotency_keys`;
-- Drop "idempotency_keys" table after copying rows
DROP TABLE `idempotency_keys`;
-- Rename temporary table "new_idempotency_keys" to "idempotency_keys"
ALTER TABLE `new_idempotency_keys` RENAME TO `idempotency_keys`;
-- Create "new_images" table
CREATE TABLE `new_images` (
  `id` text NULL,
  `blob_id` integer NOT NULL,
  `user_id` text NOT NULL,
  `token_id` integer NULL,
  `file_name` text NOT NULL DEFAULT '',
  `expire_at` text NULL,
  `last_public_access_at` text NULL,
  `pending_cleanup` integer NOT NULL DEFAULT 0,
  `cleanup_exempt` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`token_id`) REFERENCES `tokens` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `2` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Copy rows from old table "images" to new temporary table "new_images"
INSERT INTO `new_images` (`id`, `blob_id`, `user_id`, `file_name`, `expire_at`, `last_public_access_at`, `pending_cleanup`, `created_at`) SELECT `id`, `blob_id`, COALESCE(`user_id`, 'default'), `file_name`, `expire_at`, `last_public_access_at`, `pending_cleanup`, `created_at` FROM `images`;
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
-- Add column "password_hash" to table: "users"
ALTER TABLE `users` ADD COLUMN `password_hash` text NOT NULL DEFAULT '';
-- Add column "role" to table: "users"
ALTER TABLE `users` ADD COLUMN `role` text NOT NULL DEFAULT 'user';
-- Add column "quota_bytes" to table: "users"
ALTER TABLE `users` ADD COLUMN `quota_bytes` integer NULL;
-- Add column "disabled" to table: "users"
ALTER TABLE `users` ADD COLUMN `disabled` integer NOT NULL DEFAULT 0;
-- Promote the built-in default user to administrator with login name "admin"
UPDATE `users` SET `role` = 'admin', `name` = 'admin' WHERE `id` = 'default' AND `name` = 'default';
-- Create "dedup_candidates" table
CREATE TABLE `dedup_candidates` (
  `id` integer NULL,
  `blob_a` integer NOT NULL,
  `blob_b` integer NOT NULL,
  `distance` integer NOT NULL,
  `status` text NOT NULL DEFAULT 'pending',
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`blob_b`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `1` FOREIGN KEY (`blob_a`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_dedup_candidates_pair" to table: "dedup_candidates"
CREATE UNIQUE INDEX `idx_dedup_candidates_pair` ON `dedup_candidates` (`blob_a`, `blob_b`);
-- Create index "idx_dedup_candidates_status" to table: "dedup_candidates"
CREATE INDEX `idx_dedup_candidates_status` ON `dedup_candidates` (`status`);
-- Create "sessions" table
CREATE TABLE `sessions` (
  `token_hash` text NULL,
  `user_id` text NOT NULL,
  `csrf_token` text NOT NULL,
  `expires_at` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`token_hash`),
  CONSTRAINT `0` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_sessions_user" to table: "sessions"
CREATE INDEX `idx_sessions_user` ON `sessions` (`user_id`);
-- Create index "idx_sessions_expires" to table: "sessions"
CREATE INDEX `idx_sessions_expires` ON `sessions` (`expires_at`);
-- Create "tokens" table
CREATE TABLE `tokens` (
  `id` integer NULL,
  `user_id` text NOT NULL,
  `name` text NOT NULL,
  `token_hash` text NOT NULL,
  `prefix` text NOT NULL,
  `scopes` text NOT NULL DEFAULT 'upload',
  `last_used_at` text NULL,
  `revoked_at` text NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_tokens_hash" to table: "tokens"
CREATE UNIQUE INDEX `idx_tokens_hash` ON `tokens` (`token_hash`);
-- Create index "idx_tokens_user_name" to table: "tokens"
CREATE UNIQUE INDEX `idx_tokens_user_name` ON `tokens` (`user_id`, `name`) WHERE revoked_at IS NULL;
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
