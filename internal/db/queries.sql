-- name: CreateBlob :one
INSERT INTO blobs (sha256, phash, storage_key, mime, size, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: CreateImage :exec
INSERT INTO images (id, blob_id, file_name)
VALUES (?, ?, ?);

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
    attempts   = attempts + 1,
    updated_at = datetime('now')
WHERE blob_id = ? AND channel_id = ?;

-- name: ListBlobChannels :many
SELECT bc.channel_id, bc.state, c.name, c.priority, c.healthy
FROM blob_channels bc
JOIN channels c ON c.id = bc.channel_id
WHERE bc.blob_id = ?
ORDER BY c.priority DESC, c.id ASC;

-- name: ListPendingBlobChannels :many
SELECT blob_id, channel_id
FROM blob_channels
WHERE state IN ('pending', 'uploading')
ORDER BY updated_at ASC
LIMIT ?;

-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (key, image_id)
VALUES (?, ?);

-- name: GetIdempotencyImage :one
SELECT image_id
FROM idempotency_keys
WHERE key = ?;
