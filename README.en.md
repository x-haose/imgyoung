# ImgYoung

[简体中文](./README.md)

A self-hosted image host with multi-channel primary/backup failover (Go + rclone). **Work in progress: iterations 0–7 are done and the admin console has been rebuilt; the pre-1.0 release wrap-up is under way.**

What it does: **multi-channel failover** (priority-based channel selection · health checks with automatic switchover · asynchronous replica fill · instant failover on access) · **encrypted channel storage** (rclone crypt, the provider only sees ciphertext) · **web admin console** (multi-user · named tokens · quotas and rate limits · duplicate and cold-image review · audit log · data export) · on-demand processing (`?w=` resizing + WebP negotiation) · background deduplication (exact duplicates merged automatically, similar ones reviewed by hand) · TTL lifecycle · magic-bytes format validation · metadata stripping (JPEG/PNG/WebP) · pixel budget · SHA256 + perceptual hash · client idempotency · hotlink protection · proxy/302 dual mode.

Design blueprint (Chinese): [图床项目_功能与设计定稿.md](./图床项目_功能与设计定稿.md) · project conventions: [CLAUDE.md](./CLAUDE.md)

## Quick start

```bash
# Run locally (needs https://taskfile.dev and pnpm; build compiles the admin console first)
task run
# No environment variables are needed on first start: sign in with admin and the factory
# default password imgyoung123; you are forced to change it on first sign-in.
# Listens on :8721 by default (avoiding 8080 and friends); override with IMGYOUNG_LISTEN.

# Or Docker
docker build -t imgyoung .
docker run -p 8721:8721 -v imgyoung-data:/data imgyoung
```

Open `http://localhost:8721/` in a browser, sign in with `admin` and the default password `imgyoung123` (you will be asked to change it right away), issue a token on the "Upload tokens" page, and upload with it:

```bash
# Upload (optional: -H "Idempotency-Key: xx" for idempotent retries; -F exif_policy=strip to strip EXIF)
curl -H "Authorization: Bearer <token issued in the console>" -F "file=@a.png" http://localhost:8721/api/upload
# => {"id":"R0RjUyP1UW","url":"/i/R0RjUyP1UW"}

# Transfer an image from another host (fetched server-side, public addresses only)
curl -H "Authorization: Bearer <token issued in the console>" -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/photo.png"}' http://localhost:8721/api/upload/url

# Access (?w=400 picks a resize tier; WebP is served automatically when the browser supports it)
open http://localhost:8721/i/R0RjUyP1UW
```

### Clients such as PicGo and Typora

Most of these clients ship an "Amazon S3" option, and this host also accepts images over the S3 protocol, so pick that and skip the plugins: on the "Upload tokens" page click **"S3 settings"** on a token and copy the access key / secret / endpoint / custom domain from the dialog into the client (enable "path style"). The credential is the token itself: revoke it and the S3 path dies with it.

The object key the client chooses becomes an access alias: `/o/<user id>/<object key>` and the short link `/i/<short id>` point at the same image, so the Markdown links the client produces work as they are. Re-uploading the same key overwrites per S3 semantics: the alias moves to the new image while the old short link stays valid.

The S3 front implements five operations, `PutObject`, `HeadBucket`, `HeadObject`, `GetObject` and `DeleteObject`, which is enough to upload, verify, fetch and delete your own objects; bucket listing, multipart upload, ACLs and so on are not provided. Public access and management still go through `/i/`, `/o/` and the console.

## Admin console

Open `http://localhost:8721/` and sign in with `admin` and the initial password. The console provides: an overview dashboard (with unresolved alerts), image management (with replica distribution), token issuing and revocation, storage channel management, multi-user accounts with quotas, side-by-side duplicate review, cold-image cleanup review, the audit log, and settings (runtime configuration / hotlink protection / metadata export / backup snapshots and restore / alert history). The interface is bilingual (Simplified Chinese and English); switch language and theme from the top-right corner.

There are exactly two authentication fronts: **the console uses password + session** (argon2id / CSRF / login rate limiting), **the data API uses named tokens** (`Authorization: Bearer`; only hashes are stored, tokens can be revoked; the S3 front's SigV4 signatures use the same tokens, not a third kind of credential). The administrator is seeded with the factory default password `imgyoung123` (a public value that only exists so a fresh install can sign in; changing it is enforced at first sign-in); the database is the source of truth from then on. Upload tokens are always issued in the console; there is no bootstrap token held in environment variables.

### Forgot the password

Passwords are stored as argon2id hashes and cannot be written by hand, but the first-start seeding only runs "while the administrator has no password yet", so clearing that user's `password_hash` and restarting makes the process treat it as a first start and seed the factory default `imgyoung123` again:

```bash
# With the service stopped, run against imgyoung.db in the data directory (needs sqlite3 or any SQLite tool):
sqlite3 data/imgyoung.db "UPDATE users SET password_hash='' WHERE name='admin';"
```

Restart the service, sign in with `admin` and `imgyoung123`, and you will be asked to set a new password immediately.

## Configuration

Configuration is split cleanly in two, in one sentence: **environment variables only get the process started; operational parameters are changed in the console and take effect immediately**. There is no external configuration file and no koanf.

**Bootstrap configuration** (environment variables only; changes need a restart):

| Variable | Default | Description |
|---|---|---|
| `IMGYOUNG_LISTEN` | `:8721` | HTTP listen address |
| `IMGYOUNG_DATA_DIR` | `data` | Data directory (SQLite and local storage) |
| `IMGYOUNG_MASTER_KEY` | (auto-generated `data/master.key`) | Master key (64 hex characters) that encrypts channel credentials and derives channel encryption passwords; **if it is lost, channel configuration and encrypted data are unrecoverable** |
| `IMGYOUNG_TRUSTED_PROXIES` | `127.0.0.0/8,::1/128` | Reverse-proxy networks whose forwarding headers are trusted (comma-separated CIDRs or IPs). `X-Forwarded-For` / `X-Forwarded-Proto` / `X-Forwarded-Host` are honoured only when the peer is inside these networks: login rate limiting counts the real client, the session cookie gets `Secure` under HTTPS, and absolute URLs are derived from the public host when `base_url` is unset. Set your proxy's network when it runs on another machine (for example a Docker network) |
| `IMGYOUNG_METRICS_ALLOW` | loopback + private networks (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`) | Networks allowed to scrape `/metrics`; everything else gets 403. Metrics include route templates, channel names and usage, so they are not exposed to the public internet |

**Operational configuration** (the source of truth is the single-row `settings` table; edit it under "Settings → Runtime configuration" in the console, saving takes effect immediately): **environment variables are not accepted**. Configure a new instance once from the console; the database persists with the data volume. Keys and factory defaults (the hotlink whitelist is managed separately on the settings page):

| Key | Default | Description |
|---|---|---|
| `max_upload_mb` | `50` | Per-image size limit (MB) |
| `max_pixels_mp` | `50` | Pixel budget (megapixels) |
| `max_concurrent_uploads` | `8` | Upload concurrency limit; 429 when saturated |
| `upload_rate_per_min` | `0` | Uploads per user per minute; 429 beyond it; `0` = off |
| `base_url` | (empty) | Public site address; when empty, absolute URLs are derived from the request host. Set it explicitly when the public host behind a proxy / CDN differs from the request host |
| `access_mode` | `proxy` | `proxy` (proxied, instant failover) / `redirect` (302, saves bandwidth, needs channels that allow direct links) |
| `min_success` | `1` | Minimum number of synchronous replicas before an upload returns success |
| `allow_empty_referer` | `true` | Whether requests without a Referer are allowed |
| `exif_policy` | `passthrough` | Global default EXIF tier: `passthrough` (keep as is) / `strip` (remove metadata); `spoof` is experimental and not yet open. A single upload can override it with the `exif_policy` form field |
| `encrypt_new_channels` | `false` | Default encryption flag for new channels when not specified explicitly (fixed at creation) |
| `default_ttl_hours` | `0` | Global default lifetime (hours); `0` = never expires |
| `ttl_renew_on_access` | `true` | Renew on access: public access pushes the expiry of a TTL image forward by its lifetime |
| `cold_cleanup_days` | `90` | Cold-image cleanup threshold (days); `0` = off |
| `delete_grace_minutes` | `0` | Grace period from zero references to physical deletion (minutes); `0` = immediate |
| `cleanup_sweep_seconds` | `60` | Lifecycle cleanup sweep interval (seconds) |
| `activity_flush_seconds` | `60` | Access activity batch flush interval (seconds) |
| `health_check_seconds` | `60` | Channel health check interval (seconds) |
| `backup_interval_hours` | `24` | Automatic metadata database backup interval (hours); snapshots go to the highest-priority healthy channel |
| `backup_keep` | `7` | Number of backups to keep; older snapshots are cleaned up automatically |
| `usage_report_seconds` | `300` | Channel usage report interval (seconds) |
| `channel_quota_gb` | `0` | Per-channel usage alert threshold (GB); `0` = report only, no alert |
| `dedup_scan_seconds` | `300` | Background deduplication scan interval (seconds) |
| `dedup_phash_max_distance` | `10` | Maximum Hamming distance for pHash similarity; `0` = no similarity scan |
| `process_widths` | `100,200,400,800,1600` | Width tiers allowed for on-demand processing (pixels); empty = no resizing |
| `process_formats` | `webp,jpeg,png` | Target formats allowed for on-demand processing |
| `process_quality_webp` | `80` | WebP quality [0,100] |
| `process_quality_jpeg` | `85` | JPEG quality [1,100] |
| `process_webp_method` | `4` | WebP quality/speed trade-off [0,6] |
| `process_workers` | `4` | Processing concurrency limit |
| `process_timeout_ms` | `5000` | Per-request processing timeout (milliseconds); falls back to the original on timeout |
| `variants_cache_mb` | `10240` | Derived-image disk cache limit (MB); least-recently-accessed entries are evicted beyond it |
| `idempotency_key_ttl_hours` | `24` | Upload idempotency key retention (hours): the window in which a client retry with `Idempotency-Key` is honoured; expired keys are collected by the cleanup loop |
| `cdn_cloudflare_zone_id` | (empty) | Cloudflare Zone ID; when it, `cdn_cloudflare_api_token` and `base_url` are all set, deleting an image also purges the CDN cache |
| `cdn_cloudflare_api_token` | (empty) | Cloudflare API token with the Zone.Cache Purge permission; stored encrypted, never logged or returned |
| `alert_webhook_url` | (empty) | Webhook that receives alert open/resolve notifications as JSON (http or https); empty = alerts are recorded only |
| `audit_retention_days` | `0` | Audit log retention (days); entries older than this are deleted by the cleanup loop; `0` = keep forever |

## Channel management

A channel is one rclone backend instance. Uploads are written to the primary channel (the highest-priority healthy one) and return; the other channels are filled asynchronously; access fails over by priority.

Channels are created and deleted on the **"Storage channels" page of the console** (the management front uses session authentication; upload tokens cannot touch it). When creating one, give the rclone driver name and backend parameters (`path` is the path inside the backend, `public_base_url` is the 302 direct-link prefix, everything else is passed to rclone as is):

```text
# Example parameters for an S3 / R2 channel
driver:   s3
config:   provider=Cloudflare  access_key_id=...  secret_access_key=...
          endpoint=https://xxx.r2.cloudflarestorage.com  path=bucket/imgs
# Deletion guard: a channel holding the only replica of any image refuses deletion (409) to prevent data loss
```

- Deleting a channel **also removes its objects by default** (deleted one by one in the background; the channel shows as "draining" meanwhile and disappears when done). Choose "remove only" when the credentials no longer work or the bucket is gone, and the remote objects are left untouched. A channel holding the only replica of any image refuses deletion.
- A channel with `encrypted: true` stores through rclone crypt: content and file names are ciphertext and invisible to the provider; the password is derived from the master key, so with `master.key` you can decrypt independently with standard rclone tooling (self-rescue).
- The encryption setting is fixed at creation and cannot be changed afterwards (changing it would make existing objects unreadable).
- **Create the bucket in advance for S3-compatible backends (R2/MinIO and so on)**: the host sets `no_check_bucket=true` on s3 channels by default and skips rclone's bucket probe before upload, because scoped credentials usually lack bucket-creation permission, which otherwise shows up as "health probe passes but uploads return 403". If you do want rclone to create buckets, add `"no_check_bucket":"false"` to the config explicitly.

## Known limitations

- Animated WebP is not supported yet (decoder limitation; uploads return 400); animated GIF works.
- `exif_policy=strip` covers every supported format: JPEG drops metadata segments and trailing data after EOI (such as Motion Photo video), PNG drops eXIf and text chunks plus trailing data after IEND, WebP drops EXIF/XMP chunks and clears the VP8X flags accordingly; GIF has no standard metadata container and passes through.

## PicGo / PicList integration

Use [picgo-plugin-web-uploader](https://github.com/squirrel-land/picgo-plugin-web-uploader) or a PicList custom uploader with these parameters:

| Parameter | Value |
|---|---|
| API address | `https://your.domain/api/upload` (the returned `url` is derived from the request host by default; set `base_url` when the public host behind a proxy / CDN differs from the request host) |
| POST parameter name | `file` |
| Custom request header | `{"Authorization": "Bearer your-token"}` |
| JSON path | `url` |

## Development

```bash
task test   # full test suite (-race)
task e2e    # end-to-end tests: builds the real binary, starts the process, covers every public feature
task lint   # golangci-lint plus the admin console's tsc + eslint + prettier, zero issues required
task --list # list all tasks
```

## License

[MIT](./LICENSE). The admin console reuses common components from [shadcn-admin](https://github.com/satnaing/shadcn-admin); its license is at [web/licenses/shadcn-admin.LICENSE](./web/licenses/shadcn-admin.LICENSE).
