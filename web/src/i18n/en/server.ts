import type { server as zh } from '../zh-CN/server'

export const server = {
  errors: {
    internal: 'Internal server error',
    invalid_body:
      'The request body is not valid JSON, or contains unknown fields',
    unauthenticated: 'Not signed in, or the session has expired',
    csrf_rejected: 'CSRF check failed',
    admin_required: 'Administrator permission required',
    must_change_password: 'Change the initial password first',
    source_not_allowed: 'The source address is not in an allowed network',
    no_healthy_channel: 'No healthy channel is available',
    lang_invalid: 'Invalid lang (allowed: zh-CN / en)',
    query_param_invalid: 'Invalid value for query parameter {name}',
    login_rate_limited: 'Too many sign-in attempts. Try again later',
    invalid_credentials: 'Incorrect username or password',
    user_disabled: 'This user is disabled',
    old_password_incorrect: 'The current password is incorrect',
    password_too_short: 'The password must be at least {min} characters',
    user_name_taken: 'This username is already taken',
    invalid_role: 'Invalid role',
    user_not_found: 'User not found',
    user_protected:
      'The built-in administrator cannot be disabled, demoted or deleted',
    cannot_delete_self: 'You cannot delete the user you are signed in as',
    user_has_images: 'This user still owns images. Delete them first',
    user_ttl_invalid:
      'default_ttl_hours must be an integer from 0 (permanent) to {max}, or null to follow the global setting',
    token_request_invalid:
      'Invalid request: a name of at most {max_name} characters is required, and the validity in hours must be between 0 and {max_hours}',
    token_name_taken: 'A token with this name already exists',
    token_id_invalid: 'Invalid token ID',
    token_not_found: 'Token not found, or no longer valid',
    token_active: 'The token is still active. Revoke it before deleting',
    image_not_found: 'Image not found',
    image_patch_invalid:
      'Invalid request: ttl_hours must be 0 (permanent) or a positive integer up to {max}, and at least one change is required',
    cleanup_exempt_admin_only:
      'Only administrators can set the cleanup exemption',
    image_expired:
      'The image has expired, so its lifetime can no longer be changed',
    batch_ids_invalid: 'Invalid request: ids must hold 1 to {max} image IDs',
    candidate_id_invalid: 'Invalid candidate ID',
    candidate_not_found: 'Candidate not found, or already resolved',
    keeper_invalid: 'The image to keep must be one of the candidate pair',
    candidate_stale:
      'An image in this candidate has been deleted. Refresh the list',
    referer_pattern_invalid:
      'Invalid rule: use a bare domain (example.com) or a subdomain wildcard (*.example.com)',
    referer_exists: 'This rule already exists',
    referer_id_invalid: 'Invalid rule id',
    referer_not_found: 'Rule not found',
    backup_snapshot_failed: 'Creating the database snapshot failed: {detail}',
    backup_store_failed:
      'Snapshot {snapshot} was not written to {failed} of {total} channels ({detail})',
    backup_prune_failed:
      'Snapshot {snapshot} was written, but removing old snapshots failed on {failed} of {total} channels, so the retention count is not enforced ({detail})',
    snapshot_name_invalid: 'Invalid snapshot name',
    snapshot_not_found: 'Snapshot not found',
    snapshot_corrupt:
      'Snapshot {snapshot} is not a complete database file (it may have been interrupted while being written). Restore from another snapshot',
    settings_body_invalid:
      'The request body must be a non-empty JSON object of key → value',
    setting_unknown: 'Unknown setting {key}',
    setting_type: '{key} has a value of the wrong type',
    setting_positive:
      '{key}={value} is invalid: a positive integer is required',
    setting_non_negative: '{key}={value} is invalid: it cannot be negative',
    setting_too_large: '{key}={value} is invalid: it cannot exceed {max}',
    setting_range:
      '{key}={value} is invalid: an integer from {min} to {max} is required',
    setting_option: '{key}={value} is invalid: allowed values are {options}',
    setting_url: '{key}={value} is invalid: an http/https address is required',
    setting_site_root:
      '{key}={value} is invalid: a site root address like https://img.example.com is required (no path or query)',
    setting_empty: '{key} cannot be empty',
    channel_id_invalid: 'Invalid channel ID',
    channel_not_found: 'Channel not found',
    channel_name_exists: 'A channel with this name already exists',
    channel_deleting:
      'The channel is being cleaned up and cannot be changed or probed',
    channel_config_changed:
      'The channel configuration changed while saving (another edit, or the provider refreshed the authorization token). Save again',
    channel_encrypted_immutable:
      'Encryption cannot be changed after the channel is created. To change it, create a new channel and migrate the data',
    channel_sole_replicas:
      'This channel holds the only replica of {count} images; deleting it would lose data. Migrate or delete those images first',
    purge_param_invalid: 'purge must be true or false',
    channel_name_driver_required:
      'Invalid channel settings: name and driver are required',
    channel_name_too_long:
      'Invalid channel settings: name is longer than {max} bytes',
    channel_encrypted_direct_link:
      'Invalid channel settings: encrypted channels cannot allow 302 direct links',
    channel_local_path_relative:
      'Invalid channel settings: the path of a local channel must be absolute, got {path}',
    channel_config_key_invalid:
      'Invalid channel settings: parameter key {key} is invalid (only letters, digits and underscores)',
    channel_keep_not_secret:
      'Invalid channel settings: field {field} is not a credential, so its stored value cannot be kept',
    channel_keep_no_value:
      'Invalid channel settings: field {field} has no stored value to keep. Fill it in again',
    channel_unknown_backend:
      'Invalid channel settings: unknown backend {driver}',
    channel_unknown_template:
      'Invalid channel settings: unknown channel template',
    channel_field_required:
      'Invalid channel settings: field {field} is required',
    channel_param_invalid: 'Invalid channel settings: {detail}',
    channel_backend_failed:
      'Invalid channel settings: the backend failed to initialize: {detail}',
    channel_bucket_failed:
      'Creating the bucket failed (check that the key may create buckets, or create the bucket in the console first and turn off automatic creation): {detail}',
    channel_unrepresentable:
      'A parameter value cannot be written to an rclone config file: parameter {key} of remote {remote}',
    oauth_token_invalid:
      'Invalid channel settings: the authorization token is malformed',
    oauth_no_refresh_token:
      'Invalid channel settings: the authorization token has no refresh token, so it cannot be renewed once the access token expires',
    oauth_not_required:
      'Invalid channel settings: this backend does not use OAuth',
    oauth_option_required:
      'Invalid channel settings: backend {driver} requires {option} first',
    oauth_base_url_required:
      'One-click authorization requires the public site address in Settings; otherwise paste a token',
    oauth_session_not_found:
      'The authorization session does not exist or has expired',
    oauth_denied: 'Authorization was denied or canceled: {detail}',
    oauth_exchange_failed:
      'Exchanging the token failed. Check the app credentials and the callback address',
    upload_token_invalid: 'Invalid upload token',
    upload_token_scope: 'The token is not allowed to do this',
    upload_unauthenticated: 'Unauthenticated upload request',
    upload_rate_limited: 'Too many uploads. Try again later',
    upload_busy: 'The upload concurrency limit is reached. Try again later',
    upload_unsupported_type:
      'Only image files are supported (jpeg/png/gif/webp)',
    upload_too_many_pixels:
      'The image resolution exceeds the pixel budget ({max} megapixels)',
    upload_invalid_image: 'The image content cannot be decoded',
    upload_exif_policy: 'Invalid exif_policy (allowed: strip / passthrough)',
    upload_quota_exceeded: 'Storage quota is full',
    upload_unavailable:
      'Storage channels are temporarily unavailable. Try again later',
    upload_ttl_invalid:
      'Invalid ttl (an integer from 0 to {max} seconds, or never for permanent)',
    upload_too_large: 'The file exceeds the size limit ({max} MB)',
    upload_field_too_long: 'exif_policy or ttl is too long (limit {max} bytes)',
    upload_no_file:
      'The file field is missing, or the request body is not a valid multipart/form-data form',
    url_body_invalid:
      'The request body must be a JSON object with only url/file_name/exif_policy/ttl',
    url_required: 'url cannot be empty',
    url_scheme: 'url only supports http/https addresses',
    url_blocked: 'The target is not a public address and cannot be fetched',
    url_too_many_redirects: 'The remote server redirected too many times',
    url_too_large: 'The remote file exceeds the size limit ({max} MB)',
    url_bad_status:
      'The remote server returned an error status; the image could not be fetched',
    url_fetch_failed: 'Could not fetch the image from this address',
    variant_params_invalid:
      'Invalid processing parameters: w or format is not an allowed value',
  },
  failureReasons: {
    s3_auth:
      'Authentication failed (403): check the Access Key / Secret Key and their permissions',
    app_credentials:
      'Invalid app credentials: check that the Client ID / Client Secret belong to the app that issued the token',
    grant_revoked:
      'Authorization expired: the refresh token was rejected. Authorize again',
    auth: 'Authentication failed (401): check the username, password or authorization token',
    forbidden:
      'Access denied (403): check the account’s permissions on the storage location',
    bucket_missing:
      'The bucket does not exist or is not accessible: check the bucket name and credential permissions',
    region_mismatch:
      'Region mismatch: check that the selected region matches the bucket’s region',
    dns: 'Cannot resolve the endpoint host: check the endpoint setting',
    connect: 'Cannot connect to the endpoint: check its address and port',
    tls: 'TLS certificate verification failed: check the endpoint certificate or use the right protocol',
    timeout:
      'Connection timed out: check the endpoint address and network connectivity',
    permission:
      'No write permission on the storage path: check the directory permissions',
    probe_mismatch:
      'The probe object read back does not match what was written: the storage rewrote or truncated it',
    config_unreadable:
      'The channel settings cannot be decrypted: the current master key differs from the one used to save them',
    no_source: 'No readable source replica',
    channel_unavailable: 'The target channel is not loaded',
    object_missing: 'Reconciliation found the object missing from the channel',
    unclassified: 'Unclassified error',
  },
  failureStages: {
    probe: '',
    load: 'Load failed: ',
    drain:
      'Deleting the channel’s objects failed; this is retried automatically. If the credentials no longer work or the bucket is gone, remove the channel directly (remote objects are kept): ',
  },
  failureWithDetail: '{reason} ({detail})',
  alerts: {
    channel_unhealthy: '{failure}',
    replication_failed:
      '{failed} replicas failed to sync; they are retried once the channel is healthy',
    channel_quota_exceeded:
      'Usage {bytes} has exceeded the threshold {quota}; watch your bill and capacity',
    drain_failed: '{failure}',
    backup_failed: '{error}',
  },
  unhealthyUnprobed: 'The channel is unhealthy',
} satisfies typeof zh
