-- Create "audit_log" table
CREATE TABLE `audit_log` (
  `id` integer NULL,
  `actor` text NOT NULL,
  `action` text NOT NULL,
  `target` text NOT NULL DEFAULT '',
  `detail` text NOT NULL DEFAULT '',
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "idx_audit_created" to table: "audit_log"
CREATE INDEX `idx_audit_created` ON `audit_log` (`created_at`);
-- Create "blob_channels" table
CREATE TABLE `blob_channels` (
  `blob_id` integer NOT NULL,
  `channel_id` integer NOT NULL,
  `state` text NOT NULL DEFAULT 'pending',
  `attempts` integer NOT NULL DEFAULT 0,
  `last_error` text NOT NULL DEFAULT '',
  `updated_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`blob_id`, `channel_id`),
  CONSTRAINT `0` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `1` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_blob_channels_state" to table: "blob_channels"
CREATE INDEX `idx_blob_channels_state` ON `blob_channels` (`state`);
-- Create index "idx_blob_channels_channel" to table: "blob_channels"
CREATE INDEX `idx_blob_channels_channel` ON `blob_channels` (`channel_id`);
-- Create "blobs" table
CREATE TABLE `blobs` (
  `id` integer NULL,
  `sha256` text NOT NULL,
  `phash` integer NOT NULL,
  `md5` text NULL,
  `storage_key` text NOT NULL,
  `mime` text NOT NULL,
  `size` integer NOT NULL,
  `width` integer NOT NULL,
  `height` integer NOT NULL,
  `ref_count` integer NOT NULL DEFAULT 1,
  `pending_delete` integer NOT NULL DEFAULT 0,
  `delete_after` text NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "idx_blobs_sha256" to table: "blobs"
CREATE INDEX `idx_blobs_sha256` ON `blobs` (`sha256`);
-- Create index "idx_blobs_delete_after" to table: "blobs"
CREATE INDEX `idx_blobs_delete_after` ON `blobs` (`delete_after`);
-- Create "channels" table
CREATE TABLE `channels` (
  `id` integer NULL,
  `name` text NOT NULL,
  `driver` text NOT NULL,
  `config_sealed` text NOT NULL DEFAULT '',
  `priority` integer NOT NULL DEFAULT 100,
  `healthy` integer NOT NULL DEFAULT 1,
  `encrypted` integer NOT NULL DEFAULT 0,
  `allow_direct_link` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "channels_name" to table: "channels"
CREATE UNIQUE INDEX `channels_name` ON `channels` (`name`);
-- Create "idempotency_keys" table
CREATE TABLE `idempotency_keys` (
  `key` text NULL,
  `image_id` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`key`),
  CONSTRAINT `0` FOREIGN KEY (`image_id`) REFERENCES `images` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create "images" table
CREATE TABLE `images` (
  `id` text NULL,
  `blob_id` integer NOT NULL,
  `user_id` text NULL,
  `file_name` text NOT NULL DEFAULT '',
  `expire_at` text NULL,
  `last_public_access_at` text NULL,
  `pending_cleanup` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_images_blob" to table: "images"
CREATE INDEX `idx_images_blob` ON `images` (`blob_id`);
-- Create index "idx_images_expire" to table: "images"
CREATE INDEX `idx_images_expire` ON `images` (`expire_at`);
-- Create index "idx_images_access" to table: "images"
CREATE INDEX `idx_images_access` ON `images` (`last_public_access_at`);
-- Create "users" table
CREATE TABLE `users` (
  `id` text NULL,
  `name` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "users_name" to table: "users"
CREATE UNIQUE INDEX `users_name` ON `users` (`name`);
