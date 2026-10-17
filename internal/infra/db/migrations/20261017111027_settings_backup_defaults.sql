-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_settings" table
CREATE TABLE `new_settings` (
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
  `backup_interval_hours` integer NOT NULL DEFAULT 1,
  `backup_keep` integer NOT NULL DEFAULT 24,
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
-- Copy rows from old table "settings" to new temporary table "new_settings"
INSERT INTO `new_settings` (`id`, `max_upload_mb`, `max_pixels_mp`, `max_concurrent_uploads`, `upload_rate_per_min`, `base_url`, `cdn_cloudflare_zone_id`, `cdn_cloudflare_api_token`, `allow_empty_referer`, `exif_policy`, `min_success`, `access_mode`, `encrypt_new_channels`, `health_check_seconds`, `backup_interval_hours`, `backup_keep`, `cleanup_sweep_seconds`, `delete_grace_minutes`, `default_ttl_hours`, `ttl_renew_on_access`, `activity_flush_seconds`, `cold_cleanup_days`, `usage_report_seconds`, `channel_quota_gb`, `dedup_scan_seconds`, `dedup_phash_max_distance`, `process_widths`, `process_formats`, `process_quality_webp`, `process_quality_jpeg`, `process_webp_method`, `process_workers`, `process_timeout_ms`, `variants_cache_mb`, `idempotency_key_ttl_hours`, `alert_webhook_url`, `audit_retention_days`, `updated_at`) SELECT `id`, `max_upload_mb`, `max_pixels_mp`, `max_concurrent_uploads`, `upload_rate_per_min`, `base_url`, `cdn_cloudflare_zone_id`, `cdn_cloudflare_api_token`, `allow_empty_referer`, `exif_policy`, `min_success`, `access_mode`, `encrypt_new_channels`, `health_check_seconds`, IFNULL(`backup_interval_hours`, 1) AS `backup_interval_hours`, IFNULL(`backup_keep`, 24) AS `backup_keep`, `cleanup_sweep_seconds`, `delete_grace_minutes`, `default_ttl_hours`, `ttl_renew_on_access`, `activity_flush_seconds`, `cold_cleanup_days`, `usage_report_seconds`, `channel_quota_gb`, `dedup_scan_seconds`, `dedup_phash_max_distance`, `process_widths`, `process_formats`, `process_quality_webp`, `process_quality_jpeg`, `process_webp_method`, `process_workers`, `process_timeout_ms`, `variants_cache_mb`, `idempotency_key_ttl_hours`, `alert_webhook_url`, `audit_retention_days`, `updated_at` FROM `settings`;
-- Drop "settings" table after copying rows
DROP TABLE `settings`;
-- Rename temporary table "new_settings" to "settings"
ALTER TABLE `new_settings` RENAME TO `settings`;
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
