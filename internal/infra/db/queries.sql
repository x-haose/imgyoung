-- name: CreateBlob :one
INSERT INTO blobs (sha256, phash, storage_key, mime, size, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: CreateImage :exec
INSERT INTO images (id, blob_id, user_id, file_name, expire_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetImageBlob :one
SELECT i.id, i.file_name, i.blob_id, b.storage_key, b.mime, b.size
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.id = ?;

-- name: GetBlob :one
SELECT *
FROM blobs
WHERE id = ?;

-- name: GetBlobChannel :one
SELECT *
FROM blob_channels
WHERE blob_id = ? AND channel_id = ?;

-- name: CountSoleReplicas :one
SELECT COUNT(*)
FROM blob_channels bc
WHERE bc.channel_id = ?
  AND bc.state = 'done'
  AND NOT EXISTS (SELECT 1
                  FROM blob_channels o
                  WHERE o.blob_id = bc.blob_id
                    AND o.channel_id != bc.channel_id
                    AND o.state = 'done');

-- name: CreateChannel :one
INSERT INTO channels (name, driver, config_sealed, priority, encrypted, allow_direct_link)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ListChannels :many
SELECT *
FROM channels
ORDER BY priority DESC, id ASC;

-- name: GetChannel :one
SELECT *
FROM channels
WHERE id = ?;

-- name: UpdateChannelHealth :exec
UPDATE channels
SET healthy = ?
WHERE id = ?;

-- name: DeleteChannel :exec
DELETE FROM channels
WHERE id = ?;

-- name: DeleteBlobChannelsByChannel :exec
DELETE FROM blob_channels
WHERE channel_id = ?;

-- name: UpsertBlobChannel :exec
INSERT INTO blob_channels (blob_id, channel_id, state)
VALUES (?, ?, ?)
ON CONFLICT (blob_id, channel_id)
DO UPDATE SET state = excluded.state, updated_at = datetime('now');

-- name: MarkBlobChannel :exec
UPDATE blob_channels
SET state      = ?,
    last_error = ?,
    attempts   = attempts + ?,
    updated_at = datetime('now')
WHERE blob_id = ? AND channel_id = ?;

-- name: ListBlobChannels :many
SELECT bc.channel_id, bc.state, c.name, c.priority, c.healthy
FROM blob_channels bc
JOIN channels c ON c.id = bc.channel_id
WHERE bc.blob_id = ?
ORDER BY c.priority DESC, c.id ASC;

-- name: ListPendingBlobChannelsPage :many
SELECT blob_id, channel_id
FROM blob_channels
WHERE state IN ('pending', 'uploading')
ORDER BY blob_id ASC, channel_id ASC
LIMIT ? OFFSET ?;

-- name: ListDoneBlobChannelsForChannel :many
SELECT bc.blob_id, b.storage_key
FROM blob_channels bc
JOIN blobs b ON b.id = bc.blob_id
WHERE bc.channel_id = ? AND bc.state = 'done';

-- name: ResetFailedBlobChannels :exec
UPDATE blob_channels
SET state      = 'pending',
    updated_at = datetime('now')
WHERE state = 'failed' AND channel_id = ?;

-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (key, image_id)
VALUES (?, ?);

-- name: GetIdempotencyImage :one
SELECT image_id
FROM idempotency_keys
WHERE key = ?;

-- name: CreateAuditLog :exec
INSERT INTO audit_log (actor, action, target, detail)
VALUES (?, ?, ?, ?);

-- name: ListExpiredImages :many
SELECT id, blob_id
FROM images
WHERE expire_at IS NOT NULL AND expire_at < ?
ORDER BY expire_at
LIMIT ?;

-- name: DeleteIdempotencyKeysByImage :exec
DELETE FROM idempotency_keys
WHERE image_id = ?;

-- name: DeleteImage :exec
DELETE FROM images
WHERE id = ?;

-- name: DecrementBlobRef :one
UPDATE blobs
SET ref_count = ref_count - 1
WHERE id = ?
RETURNING ref_count;

-- name: MarkBlobForDelete :exec
UPDATE blobs
SET pending_delete = 1, delete_after = ?
WHERE id = ? AND ref_count <= 0;

-- name: ListBlobsToDelete :many
SELECT id, storage_key
FROM blobs
WHERE pending_delete = 1 AND delete_after IS NOT NULL AND delete_after <= ?
LIMIT ?;

-- name: ListBlobChannelIDs :many
SELECT channel_id
FROM blob_channels
WHERE blob_id = ?;

-- name: DeleteBlobChannelsByBlob :exec
DELETE FROM blob_channels
WHERE blob_id = ?;

-- name: DeleteBlob :exec
DELETE FROM blobs
WHERE id = ?;

-- name: TouchImageAccess :exec
UPDATE images
SET last_public_access_at = ?, pending_cleanup = 0
WHERE id = ?;

-- name: LabelColdImages :execrows
UPDATE images
SET pending_cleanup = 1
WHERE pending_cleanup = 0
  AND COALESCE(last_public_access_at, created_at) < ?;
