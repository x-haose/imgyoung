-- Create "alerts" table
CREATE TABLE `alerts` (
  `id` integer NULL,
  `kind` text NOT NULL,
  `target` text NOT NULL DEFAULT '',
  `message` text NOT NULL,
  `params` text NOT NULL,
  `opened_at` text NOT NULL DEFAULT (datetime('now')),
  `resolved_at` text NULL,
  `open_notified` integer NOT NULL DEFAULT 0,
  `resolve_notified` integer NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`)
);
-- Create index "idx_alerts_open" to table: "alerts"
CREATE UNIQUE INDEX `idx_alerts_open` ON `alerts` (`kind`, `target`) WHERE resolved_at IS NULL;
-- Create index "idx_alerts_opened_at" to table: "alerts"
CREATE INDEX `idx_alerts_opened_at` ON `alerts` (`opened_at`);
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
  `last_error_reason` text NOT NULL DEFAULT '',
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
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `name` text NOT NULL,
  `driver` text NOT NULL,
  `config_sealed` text NOT NULL DEFAULT '',
  `priority` integer NOT NULL DEFAULT 100,
  `healthy` integer NOT NULL DEFAULT 1,
  `encrypted` integer NOT NULL DEFAULT 0,
  `allow_direct_link` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  `bound_at` text NOT NULL DEFAULT (datetime('now')),
  `last_checked_at` text NULL,
  `last_error_stage` text NULL,
  `last_error_reason` text NULL,
  `last_error` text NULL,
  `template` text NOT NULL DEFAULT '',
  `pending_delete` integer NOT NULL DEFAULT 0
);
-- Create index "channels_name" to table: "channels"
CREATE UNIQUE INDEX `channels_name` ON `channels` (`name`);
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
-- Create "idempotency_keys" table
CREATE TABLE `idempotency_keys` (
  `user_id` text NOT NULL,
  `key` text NOT NULL,
  `image_id` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`user_id`, `key`),
  CONSTRAINT `0` FOREIGN KEY (`image_id`) REFERENCES `images` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create "images" table
CREATE TABLE `images` (
  `id` text NULL,
  `blob_id` integer NOT NULL,
  `user_id` text NOT NULL,
  `token_id` integer NULL,
  `file_name` text NOT NULL DEFAULT '',
  `object_key` text NULL,
  `expire_at` text NULL,
  `ttl_seconds` integer NULL,
  `last_public_access_at` text NULL,
  `access_count` integer NOT NULL DEFAULT 0,
  `pending_cleanup` integer NOT NULL DEFAULT 0,
  `cleanup_exempt` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`token_id`) REFERENCES `tokens` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT `2` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_images_blob" to table: "images"
CREATE INDEX `idx_images_blob` ON `images` (`blob_id`);
-- Create index "idx_images_object_key" to table: "images"
CREATE UNIQUE INDEX `idx_images_object_key` ON `images` (`user_id`, `object_key`) WHERE object_key IS NOT NULL;
-- Create index "idx_images_user" to table: "images"
CREATE INDEX `idx_images_user` ON `images` (`user_id`);
-- Create index "idx_images_token" to table: "images"
CREATE INDEX `idx_images_token` ON `images` (`token_id`);
-- Create index "idx_images_expire" to table: "images"
CREATE INDEX `idx_images_expire` ON `images` (`expire_at`);
-- Create index "idx_images_access" to table: "images"
CREATE INDEX `idx_images_access` ON `images` (`last_public_access_at`);
-- Create "master_key_fingerprint" table
CREATE TABLE `master_key_fingerprint` (
  `id` integer NULL,
  `fingerprint` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CHECK (id = 1)
);
-- Create "referer_whitelist" table
CREATE TABLE `referer_whitelist` (
  `id` integer NULL,
  `pattern` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "referer_whitelist_pattern" to table: "referer_whitelist"
CREATE UNIQUE INDEX `referer_whitelist_pattern` ON `referer_whitelist` (`pattern`);
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
-- Create "settings" table
CREATE TABLE `settings` (
  `id` integer NULL,
  `max_upload_mb` integer NOT NULL DEFAULT 50,
  `max_pixels_mp` integer NOT NULL DEFAULT 50,
  `max_concurrent_uploads` integer NOT NULL DEFAULT 8,
  `upload_rate_per_min` integer NOT NULL DEFAULT 0,
  `base_url` text NOT NULL DEFAULT '',
  `cdn_cloudflare_zone_id` text NOT NULL DEFAULT '',
  `cdn_cloudflare_api_token` text NOT NULL DEFAULT '',
  `allow_empty_referer` integer NOT NULL DEFAULT 1,
  `exif_policy` text NOT NULL DEFAULT 'passthrough',
  `min_success` integer NOT NULL DEFAULT 1,
  `access_mode` text NOT NULL DEFAULT 'proxy',
  `encrypt_new_channels` integer NOT NULL DEFAULT 0,
  `health_check_seconds` integer NOT NULL DEFAULT 60,
  `backup_interval_hours` integer NOT NULL DEFAULT 24,
  `backup_keep` integer NOT NULL DEFAULT 7,
  `cleanup_sweep_seconds` integer NOT NULL DEFAULT 60,
  `delete_grace_minutes` integer NOT NULL DEFAULT 0,
  `default_ttl_hours` integer NOT NULL DEFAULT 0,
  `ttl_renew_on_access` integer NOT NULL DEFAULT 1,
  `activity_flush_seconds` integer NOT NULL DEFAULT 60,
  `cold_cleanup_days` integer NOT NULL DEFAULT 90,
  `usage_report_seconds` integer NOT NULL DEFAULT 300,
  `channel_quota_gb` integer NOT NULL DEFAULT 0,
  `dedup_scan_seconds` integer NOT NULL DEFAULT 300,
  `dedup_phash_max_distance` integer NOT NULL DEFAULT 10,
  `process_widths` text NOT NULL DEFAULT '[100,200,400,800,1600]',
  `process_formats` text NOT NULL DEFAULT '["webp","jpeg","png"]',
  `process_quality_webp` integer NOT NULL DEFAULT 80,
  `process_quality_jpeg` integer NOT NULL DEFAULT 85,
  `process_webp_method` integer NOT NULL DEFAULT 4,
  `process_workers` integer NOT NULL DEFAULT 4,
  `process_timeout_ms` integer NOT NULL DEFAULT 5000,
  `variants_cache_mb` integer NOT NULL DEFAULT 10240,
  `idempotency_key_ttl_hours` integer NOT NULL DEFAULT 24,
  `alert_webhook_url` text NOT NULL DEFAULT '',
  `audit_retention_days` integer NOT NULL DEFAULT 0,
  `updated_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CHECK (id = 1)
);
-- Create "tokens" table
CREATE TABLE `tokens` (
  `id` integer NULL,
  `user_id` text NOT NULL,
  `name` text NOT NULL,
  `token_hash` text NOT NULL,
  `prefix` text NOT NULL,
  `scopes` text NOT NULL DEFAULT 'upload',
  `expires_at` text NULL,
  `last_used_at` text NULL,
  `revoked_at` text NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_tokens_hash" to table: "tokens"
CREATE UNIQUE INDEX `idx_tokens_hash` ON `tokens` (`token_hash`);
-- Create index "idx_tokens_prefix" to table: "tokens"
CREATE UNIQUE INDEX `idx_tokens_prefix` ON `tokens` (`prefix`);
-- Create index "idx_tokens_user_name" to table: "tokens"
CREATE UNIQUE INDEX `idx_tokens_user_name` ON `tokens` (`user_id`, `name`) WHERE revoked_at IS NULL;
-- Create "users" table
CREATE TABLE `users` (
  `id` text NULL,
  `name` text NOT NULL,
  `password_hash` text NOT NULL DEFAULT '',
  `must_change_password` integer NOT NULL DEFAULT 0,
  `role` text NOT NULL DEFAULT 'user',
  `quota_bytes` integer NULL,
  `default_ttl_hours` integer NULL,
  `disabled` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "users_name" to table: "users"
CREATE UNIQUE INDEX `users_name` ON `users` (`name`);
-- Create "variants" table
CREATE TABLE `variants` (
  `id` integer NULL,
  `blob_id` integer NOT NULL,
  `params_hash` text NOT NULL,
  `width` integer NOT NULL DEFAULT 0,
  `format` text NOT NULL,
  `size` integer NOT NULL,
  `last_access` text NOT NULL DEFAULT (datetime('now')),
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_variants_key" to table: "variants"
CREATE UNIQUE INDEX `idx_variants_key` ON `variants` (`blob_id`, `params_hash`);
-- Create index "idx_variants_last_access" to table: "variants"
CREATE INDEX `idx_variants_last_access` ON `variants` (`last_access`);
-- Create index "idx_variants_blob" to table: "variants"
CREATE INDEX `idx_variants_blob` ON `variants` (`blob_id`);
