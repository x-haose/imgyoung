-- name: GetSettings :one
SELECT *
FROM settings
WHERE id = 1;

-- name: EnsureSettingsRow :exec
INSERT OR IGNORE INTO settings (id)
VALUES (1);

-- name: UpdateSettings :exec
UPDATE settings
SET max_upload_mb = ?,
    max_pixels_mp = ?,
    max_concurrent_uploads = ?,
    upload_rate_per_min = ?,
    base_url = ?,
    cdn_cloudflare_zone_id = ?,
    cdn_cloudflare_api_token = ?,
    allow_empty_referer = ?,
    exif_policy = ?,
    min_success = ?,
    access_mode = ?,
    encrypt_new_channels = ?,
    health_check_seconds = ?,
    backup_interval_hours = ?,
    backup_keep = ?,
    cleanup_sweep_seconds = ?,
    delete_grace_minutes = ?,
    default_ttl_hours = ?,
    ttl_renew_on_access = ?,
    activity_flush_seconds = ?,
    cold_cleanup_days = ?,
    usage_report_seconds = ?,
    channel_quota_gb = ?,
    dedup_scan_seconds = ?,
    dedup_phash_max_distance = ?,
    process_widths = ?,
    process_formats = ?,
    process_quality_webp = ?,
    process_quality_jpeg = ?,
    process_webp_method = ?,
    process_workers = ?,
    process_timeout_ms = ?,
    variants_cache_mb = ?,
    idempotency_key_ttl_hours = ?,
    alert_webhook_url = ?,
    audit_retention_days = ?,
    updated_at = datetime('now')
WHERE id = 1;
