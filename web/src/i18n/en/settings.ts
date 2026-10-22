import type { settings as zh } from '../zh-CN/settings'

export const settings = {
  title: 'Settings',
  description:
    'Runtime settings take effect on save, no restart needed. Hotlink protection, backups and alert history are here too.',
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
        hint: 'Access mode, public address, replicas and encryption',
      },
      upload: {
        title: 'Upload limits',
        hint: 'Size, pixels, concurrency and rate',
      },
      lifecycle: {
        title: 'Lifecycle',
        hint: 'Expiry, cold images, deletion and background scans',
      },
      process: {
        title: 'On-demand processing',
        hint: 'Resizing and format conversion accept only the values below; requests that need a variant are always served by this service, even in redirect mode',
      },
      dedup: {
        title: 'Deduplication',
        hint: 'Identical images merge automatically; similar ones go to Duplicates',
      },
      reliability: {
        title: 'Reliability & channels',
        hint: 'Health checks, backups and usage',
      },
      cdn: {
        title: 'CDN cache purge',
        hint: 'Purges the Cloudflare cache when images are deleted or expire; needs the Zone ID, API token and public site address',
      },
      alerts: {
        title: 'Alerts & audit',
        hint: 'Alert webhook and audit log retention',
      },
    },
    fields: {
      access_mode: {
        label: 'Access mode',
        hint: 'redirect sends original-image requests to a direct-link channel with a 302, saving bandwidth; requests that need processing, and images with no direct-link channel, are still served here',
      },
      base_url: {
        label: 'Public site address',
        hint: 'Site root used in image links; empty derives it from the request. Required when a proxy or CDN changes the domain, for CDN cache purge and for one-click cloud drive authorization',
      },
      min_success: {
        label: 'Minimum synced replicas',
        hint: 'Channels that must be written before an upload succeeds; uploads fail when too few channels are healthy',
      },
      encrypt_new_channels: {
        label: 'Encrypt new channels by default',
        hint: 'Whether a new channel is encrypted when its server-side encryption is left at Inherit the global default',
      },
      max_upload_mb: {
        label: 'Max image size',
        hint: 'Larger uploads are rejected',
      },
      max_pixels_mp: {
        label: 'Pixel budget',
        hint: 'Width × height limit per image; larger images are rejected',
      },
      max_concurrent_uploads: {
        label: 'Concurrent uploads',
        hint: 'Shared by every upload path; requests beyond it get 429',
      },
      upload_rate_per_min: {
        label: 'Upload rate limit',
        hint: 'Uploads per user per minute; 0 means no limit',
      },
      default_ttl_hours: {
        label: 'Default lifetime',
        hint: 'Used when neither the upload nor the user sets a lifetime; 0 means never expires',
      },
      ttl_renew_on_access: {
        label: 'Renew on access',
        hint: 'Public access pushes the expiry of a time-limited image forward by its lifetime',
      },
      cold_cleanup_days: {
        label: 'Cold image threshold',
        hint: 'Images without public access for this many days go to Cleanup and are never deleted automatically; 0 disables',
      },
      delete_grace_minutes: {
        label: 'Delete grace period',
        hint: 'How long to wait after an image is no longer referenced before deleting its objects; 0 means at the next cleanup sweep',
      },
      cleanup_sweep_seconds: {
        label: 'Cleanup sweep interval',
        hint: 'Interval for reclaiming expired images, deleting objects and flagging cold images',
      },
      activity_flush_seconds: {
        label: 'Activity flush interval',
        hint: 'Access counts and last-access times are saved in batches at this interval',
      },
      idempotency_key_ttl_hours: {
        label: 'Idempotency key retention',
        hint: 'Uploads retried with the same Idempotency-Key within this window return the first result',
      },
      process_widths: {
        label: 'Width tiers',
        hint: 'Allowed ?w= values in pixels, comma separated; empty disallows resizing',
      },
      process_formats: {
        label: 'Target formats',
        hint: 'Allowed ?format= values; GIF images are always served as is',
      },
      exif_policy: {
        label: 'Default EXIF policy',
        hint: 'Whether uploads keep or strip EXIF and other metadata; a per-upload form field can override it, and S3 uploads are always stored as is',
      },
      process_quality_webp: { label: 'WebP quality', hint: '1 to 100' },
      process_quality_jpeg: { label: 'JPEG quality', hint: '1 to 100' },
      process_webp_method: {
        label: 'WebP effort',
        hint: '0 to 6; higher encodes slower and smaller',
      },
      process_workers: {
        label: 'Processing concurrency',
        hint: 'Requests processed at the same time',
      },
      process_timeout_ms: {
        label: 'Processing timeout',
        hint: 'The original is served on timeout',
      },
      variants_cache_mb: {
        label: 'Variant cache limit',
        hint: 'Disk limit for the variant cache; the least recently accessed variants are evicted first',
      },
      dedup_scan_seconds: {
        label: 'Dedup scan interval',
        hint: 'Each scan merges identical images first, then looks for similar ones',
      },
      dedup_phash_max_distance: {
        label: 'Similarity threshold',
        hint: 'Higher is looser; 0 disables similarity scanning',
      },
      health_check_seconds: {
        label: 'Health check interval',
        hint: 'Interval for channel health probes and alert checks',
      },
      backup_interval_hours: {
        label: 'Backup interval',
        hint: 'Interval between automatic metadata snapshots',
      },
      backup_keep: {
        label: 'Backups to keep',
        hint: 'Snapshots kept per channel; the oldest are deleted beyond this',
      },
      usage_report_seconds: {
        label: 'Usage report interval',
        hint: 'Refresh interval of the channel usage metric',
      },
      channel_quota_gb: {
        label: 'Channel usage alert threshold',
        hint: 'Opens an alert when a channel exceeds it, without blocking writes; 0 disables',
      },
      replicate_limit_mbps: {
        label: 'Replication write limit',
        hint: 'Total bandwidth cap for background replication writes across all channels; 0 means unlimited. Only background replication is limited, not user uploads',
      },
      cdn_cloudflare_zone_id: {
        label: 'Cloudflare Zone ID',
        hint: 'Empty disables',
      },
      cdn_cloudflare_api_token: {
        label: 'Cloudflare API token',
        hint: 'Needs the Zone.Cache Purge permission; stored encrypted and never shown after saving',
      },
      alert_webhook_url: {
        label: 'Alert webhook URL',
        hint: 'Receives a JSON POST when an alert opens or resolves; empty sends nothing',
      },
      audit_retention_days: {
        label: 'Audit log retention',
        hint: 'Older entries are deleted by the cleanup sweep; 0 keeps them forever',
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
      mbps: 'MB/s',
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
      copies: 'copies',
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
    purgeEnabled: 'Enabled',
    purgeDisabled: 'Disabled',
    clearToken: 'Clear token',
    clearTokenTitle: 'Clear the configured API token?',
    clearTokenDesc:
      'CDN purge stops immediately; enabling it again requires entering a token.',
    tokenCleared: 'Cloudflare API token cleared',
    bootstrap: {
      title: 'Bootstrap configuration',
      hint: 'Read-only, from the IMGYOUNG_* environment variables; changes need a process restart',
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
    desc: 'Only requests whose Referer matches the allowlist can load public image links (/i/ and /o/); an empty allowlist turns hotlink protection off. Changes apply immediately.',
    allowEmpty: 'Allow empty referer',
    allowEmptyHint:
      'With a non-empty allowlist, whether requests without a Referer are allowed, such as opening a link in the address bar or using a download tool',
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
      'Takes effect immediately and the remaining rules apply; removing the last rule turns hotlink protection off.',
    removed: 'Removed {pattern}',
  },
  export: {
    title: 'Data export',
    desc: 'Exports users, channels, images, replica placement and other metadata, without credentials. Even without ImgYoung, it lets you match the objects in your channels back to the original images; to restore an instance, use a snapshot.',
    keyWarningTitle: 'Back up the master key separately',
    keyWarningDesc:
      'The master key (master.key in the data directory, or the value of IMGYOUNG_MASTER_KEY) is not in the export or the snapshots. Without it, channel credentials and the data on encrypted channels cannot be recovered. Copy it to a separate safe place.',
    download: 'Export JSON',
  },
  backup: {
    title: 'Metadata snapshots',
    desc: 'Backed up automatically to every healthy channel, each keeping its own most recent copies; take, download or restore a snapshot here.',
    createNow: 'Back up now',
    created: 'Backed up: {name}',
    createProxyTimeout:
      'The reverse proxy timed out, but the backup may still be running on the server. Refresh the snapshot list later and back up again only if no new snapshot appears.',
    name: 'Snapshot',
    size: 'Size',
    takenAt: 'Taken',
    channels: 'Stored on',
    download: 'Download',
    restore: 'Restore',
    empty: 'No snapshots yet',
    restoreTitle: 'Restore the metadata database from {name}?',
    restoreDesc:
      'Once the snapshot is downloaded and verified, the service stops, and the restore completes when a process manager such as systemd or Docker restarts it; without one, start the service by hand. Accounts and passwords also return to the snapshot, and the current database is kept as imgyoung.db.pre-restore-<time>.',
    restoreConfirm: 'Restore and restart',
    restoreProxyTimeout:
      'The reverse proxy timed out, so the restore was aborted and nothing changed. For a large snapshot, raise the proxy read timeout (such as Nginx proxy_read_timeout), or bypass the proxy and restore again.',
    restoreCdnTimeout:
      'Cloudflare timed out (524), so it is unknown whether the restore completed. If Cloudflare kept its connection to the origin open, the restore completes there and the service restarts on its own; otherwise it was aborted. If the snapshot takes longer than 125 seconds to download, restore from the admin console on the origin directly.',
    restarting: 'Service restarting',
    restartingDesc:
      'The snapshot passed verification; waiting for the service to restart. The page reloads automatically once it is back.',
    restartSlow:
      'Still down after a minute? Check the process manager or start the service by hand.',
  },
  recover: {
    title: 'Recover image records',
    desc: 'When the metadata database goes back to an earlier state (restored from a snapshot, or recent writes had not been replicated yet), images uploaded afterwards are still in the channels but their links return 404. This registers them again from the channel objects, and the original links work again.',
    note: 'Recovered images belong to the current account and never expire; file names and source tokens cannot be recovered. Safe to run again: records already registered are skipped.',
    run: 'Recover image records',
    done: 'Recovery finished: {recovered} registered',
    scanned: 'Image objects in channels',
    recorded: 'Already recorded',
    recovered: 'Recovered now',
    failed: 'Not recovered',
    failuresTitle: 'Objects not recovered',
    key: 'Object key',
    reason: 'Reason',
    truncated: 'Showing the first {shown} of {total}',
    reasons: {
      read_failed: 'Reading from the channels failed; retry once they are back',
      too_large: 'Larger than the current per-image size limit',
      invalid_image: 'Not a decodable image, or over the pixel budget',
      key_mismatch: 'Content does not match the hash in the object key',
      id_taken: 'The short ID already belongs to another image record',
      register_failed: 'Registering failed',
    },
  },
  alerts: {
    title: 'Alert history',
    desc: 'Every alert opening and resolving is recorded here, and sent to the alert webhook when one is configured.',
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
