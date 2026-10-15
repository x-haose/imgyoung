import type { settings as zh } from '../zh-CN/settings'

export const settings = {
  title: 'Settings',
  description:
    'Runtime settings apply as soon as they are saved; hotlink protection, backups and alert history live here too.',
  tabs: {
    config: 'Runtime settings',
    referer: 'Hotlink protection',
    backup: 'Backups & export',
    alerts: 'Alert history',
  },
  config: {
    hotReload: 'Applies on save, no restart needed',
    changes: '{count} changed',
    noChanges: 'No changes',
    invalid: '{count} invalid',
    revert: 'Revert',
    save: 'Save changes',
    saved: 'Saved {count} changes, effective immediately',
    required: 'Required',
    integer: 'Must be an integer',
    min: 'Must be at least {min}',
    max: 'Must be at most {max}',
    widthsInvalid: 'Widths must be positive integers separated by commas',
    formatsRequired: 'Allow at least one target format',
    urlInvalid: 'Must be an http/https URL with a host',
    edited: 'Edited',
    groups: {
      access: {
        title: 'Access & links',
        hint: 'Public address and replica threshold',
      },
      upload: { title: 'Upload limits', hint: 'Size, pixels and concurrency' },
      lifecycle: {
        title: 'Lifecycle',
        hint: 'Expiry, cold images and cleanup cadence',
      },
      process: {
        title: 'On-demand processing',
        hint: 'Requests with processing parameters are always proxied, even in redirect mode; whitelisted tiers keep arbitrary parameters from saturating the CPU',
      },
      dedup: {
        title: 'Deduplication',
        hint: 'Exact duplicates merge automatically, similar images become candidates',
      },
      reliability: {
        title: 'Reliability & channels',
        hint: 'Probes, backups and usage',
      },
      cdn: {
        title: 'CDN cache purge',
        hint: 'Cloudflare purge on delete; needs the Zone ID, API token and public site address together',
      },
      alerts: {
        title: 'Alerts & audit',
        hint: 'Alert delivery and log retention',
      },
    },
    fields: {
      access_mode: {
        label: 'Access mode',
        hint: 'proxy serves through this service; redirect sends a 302 to channels that allow direct links, saving bandwidth, while requests with processing parameters, and images no direct-link channel holds, are still proxied',
      },
      base_url: {
        label: 'Public site address',
        hint: 'Leave empty to derive from the request host; required when the public domain behind a proxy or CDN differs from the request host',
      },
      min_success: {
        label: 'Minimum synced replicas',
        hint: 'Channels written before an upload is reported successful',
      },
      encrypt_new_channels: {
        label: 'Encrypt new channels by default',
        hint: 'Default when a new channel does not choose explicitly',
      },
      max_upload_mb: {
        label: 'Max image size',
        hint: 'Larger uploads are rejected',
      },
      max_pixels_mp: {
        label: 'Pixel budget',
        hint: 'Width × height ceiling, keeps huge images from exhausting memory',
      },
      max_concurrent_uploads: {
        label: 'Concurrent uploads',
        hint: 'Shared globally, determines peak memory',
      },
      upload_rate_per_min: {
        label: 'Upload rate limit',
        hint: 'Per user; 0 disables',
      },
      default_ttl_hours: {
        label: 'Default lifetime',
        hint: '0 means never expires; per-upload and per-user defaults override it',
      },
      ttl_renew_on_access: {
        label: 'Renew on access',
        hint: 'Public access pushes the expiry of a time-limited image forward by its lifetime',
      },
      cold_cleanup_days: {
        label: 'Cold image threshold',
        hint: 'Images without public access for this long enter the cleanup queue; 0 disables',
      },
      delete_grace_minutes: {
        label: 'Delete grace period',
        hint: 'Wait between the last reference going away and physical deletion; 0 means immediately',
      },
      cleanup_sweep_seconds: {
        label: 'Cleanup sweep interval',
        hint: 'Cadence of expiry and cold-image scans',
      },
      activity_flush_seconds: {
        label: 'Activity flush interval',
        hint: 'Batch write interval for access counts and last-access times',
      },
      idempotency_key_ttl_hours: {
        label: 'Idempotency key retention',
        hint: 'Retry window for clients sending Idempotency-Key; expired keys are reclaimed',
      },
      process_widths: {
        label: 'Width tiers',
        hint: 'Allowed ?w= values in pixels, comma separated; empty disallows resizing',
      },
      process_formats: {
        label: 'Target formats',
        hint: 'Allowed ?format= values; gif is only passed through',
      },
      exif_policy: {
        label: 'Default EXIF policy',
        hint: 'A per-upload form field can override it; S3 uploads are always stored byte for byte',
      },
      process_quality_webp: { label: 'WebP quality', hint: '0 to 100' },
      process_quality_jpeg: { label: 'JPEG quality', hint: '1 to 100' },
      process_webp_method: {
        label: 'WebP effort',
        hint: '0 to 6; higher is slower and smaller',
      },
      process_workers: {
        label: 'Processing concurrency',
        hint: 'Requests processed at the same time',
      },
      process_timeout_ms: {
        label: 'Processing timeout',
        hint: 'Falls back to the original on timeout',
      },
      variants_cache_mb: {
        label: 'Variant cache limit',
        hint: 'Least recently used variants are evicted beyond this',
      },
      dedup_scan_seconds: {
        label: 'Dedup scan interval',
        hint: 'Background scan cadence',
      },
      dedup_phash_max_distance: {
        label: 'Similarity threshold',
        hint: 'Higher is looser; 0 disables similarity scanning',
      },
      health_check_seconds: {
        label: 'Health check interval',
        hint: 'Cadence of channel probes and alert evaluation',
      },
      backup_interval_hours: {
        label: 'Backup interval',
        hint: 'Interval between automatic metadata snapshots',
      },
      backup_keep: {
        label: 'Backups to keep',
        hint: 'Losing the mapping makes everything untraceable, so keep more rather than fewer',
      },
      usage_report_seconds: {
        label: 'Usage report interval',
        hint: 'Refresh cadence of channel usage metrics',
      },
      channel_quota_gb: {
        label: 'Channel usage alert threshold',
        hint: 'Opens an alert when exceeded; 0 reports without alerting',
      },
      cdn_cloudflare_zone_id: {
        label: 'Cloudflare Zone ID',
        hint: 'Empty disables',
      },
      cdn_cloudflare_api_token: {
        label: 'Cloudflare API token',
        hint: 'Needs the Zone.Cache Purge permission; stored encrypted and never shown again',
      },
      alert_webhook_url: {
        label: 'Alert webhook URL',
        hint: 'Receives a JSON POST when an alert opens or resolves; empty disables',
      },
      audit_retention_days: {
        label: 'Audit log retention',
        hint: 'Older entries are reclaimed by the cleanup loop; 0 keeps them forever',
      },
    },
    placeholders: {
      base_url: 'https://img.example.com',
      process_widths: '100, 200, 400, 800, 1600',
      alert_webhook_url: 'https://hooks.example.com/imgyoung',
    },
    units: {
      mb: 'MB',
      gb: 'GB',
      megapixels: 'megapixels',
      count: '',
      perMinute: '/ min',
      hours: 'h',
      days: 'days',
      minutes: 'min',
      seconds: 's',
      ms: 'ms',
      hamming: 'Hamming distance',
      replicas: 'replicas',
    },
    options: {
      access_mode: {
        proxy: 'proxy · serve through this host',
        redirect: 'redirect · 302 to direct links',
      },
      exif_policy: {
        passthrough: 'passthrough · keep',
        strip: 'strip · remove metadata',
      },
    },
    tokenSet: 'Configured; leave empty to keep',
    tokenUnset: 'Not configured',
    purgeEnabled: 'Purge enabled',
    purgeDisabled: 'Purge disabled',
    clearToken: 'Clear token',
    clearTokenTitle: 'Clear the configured API token?',
    clearTokenDesc:
      'CDN purge stops immediately; enabling it again requires entering a token.',
    tokenCleared: 'Cloudflare API token cleared',
    bootstrap: {
      title: 'Bootstrap configuration',
      hint: 'IMGYOUNG_* environment variables; changes need a process restart',
      listen: 'Listen address',
      data_dir: 'Data directory',
      trusted_proxies: 'Trusted proxy networks',
      metrics_allow: 'Networks allowed on /metrics',
      version: 'Server version',
      commit: 'Build commit',
      builtAt: 'Built at',
      unset: 'Not set',
      trustNone: 'Empty: forwarded headers are never trusted',
      metricsNone: 'Empty: open to no one',
      builtin: 'Built-in policies',
      loginRate:
        'Login rate limit: a burst of {burst} attempts per IP / username, then one more every {seconds} s',
      reconcile: 'Replica reconciliation every {seconds} s',
    },
  },
  referer: {
    title: 'Hotlink protection',
    desc: 'Only referers matching the whitelist can load public image links (/i/ and /o/); changes apply immediately.',
    allowEmpty: 'Allow empty referer',
    allowEmptyHint:
      'With the whitelist enabled, whether direct requests without a Referer (address bar, download clients) are allowed',
    allowEmptySaved: 'Empty referer policy updated',
    rules: 'Active rules',
    disabled: 'Hotlink protection is off; every referer is allowed.',
    syntax:
      'A bare domain such as example.com or a wildcard such as *.example.com',
    patternPlaceholder: '*.example.com',
    patternInvalid:
      'Invalid rule: use a bare domain (example.com) or a wildcard (*.example.com)',
    add: 'Add',
    added: 'Rule added, effective immediately',
    remove: 'Remove rule {pattern}',
    removeTitle: 'Remove rule {pattern}?',
    removeDesc:
      'Takes effect immediately; matching referers are then judged by the remaining rules.',
    removed: 'Removed {pattern}',
  },
  export: {
    title: 'Data export',
    desc: 'Exports all metadata (users, channels, images, replica placement; credentials redacted). Images and the ledger stay in your channels, so you can recover even if the project stops.',
    keyWarningTitle: 'Back up the master key file separately',
    keyWarningDesc:
      'The master key (master.key in the data directory) is not part of the export or remote snapshots; without it channel credentials and encrypted channel data are lost for good. Copy it to a separate safe place.',
    download: 'Export JSON',
  },
  backup: {
    title: 'Metadata snapshots',
    desc: 'Backed up automatically to every healthy channel, each keeping its own most recent copies; take, download or restore a snapshot here.',
    createNow: 'Back up now',
    created: 'Backed up: {name}',
    name: 'Snapshot',
    size: 'Size',
    takenAt: 'Taken',
    channels: 'Stored on',
    download: 'Download',
    restore: 'Restore',
    empty: 'No snapshots yet',
    restoreTitle: 'Restore the metadata database from {name}?',
    restoreDesc:
      'The snapshot is staged in the data directory and the service shuts down gracefully; a process manager such as systemd or Docker swaps it in when it restarts the service. Without one, start the service by hand. The current database is kept as imgyoung.db.pre-restore-<time>, nothing is lost.',
    restoreConfirm: 'Restore and restart',
    restarting: 'Service restarting',
    restartingDesc:
      'The snapshot is staged; waiting for the process to come back. The page reloads automatically once it does.',
    restartSlow:
      'Still down after a minute? Check the process manager or start the service by hand.',
  },
  alerts: {
    title: 'Alert history',
    desc: 'Alerts are evaluated by the health check loop; both opening and resolving are recorded, and delivered to the webhook when one is configured.',
    status: 'Status',
    open: 'Open',
    resolved: 'Resolved',
    kind: 'Kind',
    target: 'Target',
    message: 'Details',
    openedAt: 'Opened',
    resolvedAt: 'Resolved',
    empty: 'No alerts recorded',
    kinds: {
      channel_unhealthy: 'Channel unhealthy',
      replication_failed: 'Replication failed',
      channel_quota_exceeded: 'Channel over quota',
      drain_failed: 'Channel drain failed',
      backup_failed: 'Backup failed',
    },
  },
} satisfies typeof zh
