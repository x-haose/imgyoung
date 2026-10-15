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
                           JOIN channels c ON c.id = o.channel_id
                  WHERE o.blob_id = bc.blob_id
                    AND o.channel_id != bc.channel_id
                    AND o.state = 'done'
                    AND c.pending_delete = 0);

-- name: ListBlobChannelsForChannelPage :many
SELECT bc.blob_id, b.storage_key
FROM blob_channels bc
         JOIN blobs b ON b.id = bc.blob_id
WHERE bc.channel_id = ?
ORDER BY bc.blob_id
LIMIT ?;

-- name: DeleteBlobChannel :exec
DELETE FROM blob_channels
WHERE blob_id = ? AND channel_id = ?;

-- name: DeleteBlobChannelsByChannel :exec
DELETE FROM blob_channels
WHERE channel_id = ?;

-- name: UpsertBlobChannel :exec
INSERT INTO blob_channels (blob_id, channel_id, state)
VALUES (?, ?, ?)
ON CONFLICT (blob_id, channel_id)
DO UPDATE SET state = excluded.state, updated_at = datetime('now');

-- name: InsertBlobChannelInService :execrows
INSERT INTO blob_channels (blob_id, channel_id, state)
SELECT sqlc.arg(blob_id), c.id, sqlc.arg(state)
FROM channels c
WHERE c.id = sqlc.arg(channel_id) AND c.pending_delete = 0;

-- name: InsertBlobChannelIfExists :execrows
INSERT INTO blob_channels (blob_id, channel_id, state)
SELECT sqlc.arg(blob_id), c.id, sqlc.arg(state)
FROM channels c
WHERE c.id = sqlc.arg(channel_id);

-- name: MarkBlobChannel :exec
UPDATE blob_channels
SET state             = ?,
    last_error_reason = ?,
    last_error        = ?,
    attempts          = attempts + ?,
    updated_at        = datetime('now')
WHERE blob_id = ? AND channel_id = ?;

-- name: ListBlobChannels :many
SELECT bc.channel_id, bc.state, c.name, c.priority, c.healthy
FROM blob_channels bc
JOIN channels c ON c.id = bc.channel_id
WHERE bc.blob_id = ?
ORDER BY c.priority DESC, c.id ASC;

-- name: ListPendingBlobChannelsAfter :many
SELECT blob_id, channel_id
FROM blob_channels
WHERE state IN ('pending', 'uploading')
  AND (blob_id, channel_id) > (sqlc.arg(after_blob_id), sqlc.arg(after_channel_id))
ORDER BY blob_id ASC, channel_id ASC
LIMIT sqlc.arg(page_size);

-- name: ListBlobChannelKeysForChannel :many
SELECT bc.blob_id, b.storage_key, bc.state
FROM blob_channels bc
JOIN blobs b ON b.id = bc.blob_id
WHERE bc.channel_id = ?;

-- name: ResetFailedBlobChannels :exec
UPDATE blob_channels
SET state      = 'pending',
    updated_at = datetime('now')
WHERE state = 'failed' AND channel_id = ?;

-- name: ListBlobChannelIDs :many
SELECT channel_id
FROM blob_channels
WHERE blob_id = ?;

-- name: DeleteBlobChannelsByBlob :exec
DELETE FROM blob_channels
WHERE blob_id = ?;

-- name: ChannelReplicaStats :many
SELECT channel_id,
       CAST(SUM(CASE WHEN state = 'done' THEN 1 ELSE 0 END) AS INTEGER)                    AS done,
       CAST(SUM(CASE WHEN state IN ('pending', 'uploading') THEN 1 ELSE 0 END) AS INTEGER) AS pending,
       CAST(SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END) AS INTEGER)                  AS failed
FROM blob_channels
GROUP BY channel_id;

-- name: SeedBlobChannelsForChannel :execrows
INSERT INTO blob_channels (blob_id, channel_id, state)
SELECT b.id, c.id, 'pending'
FROM blobs b
         CROSS JOIN channels c
WHERE c.id = ?
  AND b.pending_delete = 0
  AND NOT EXISTS (SELECT 1
                  FROM blob_channels bc
                  WHERE bc.blob_id = b.id
                    AND bc.channel_id = c.id);

-- name: ListImageReplicas :many
SELECT bc.channel_id, c.name AS channel_name, bc.state, bc.attempts, bc.last_error_reason, bc.last_error, bc.updated_at
FROM images i
JOIN blob_channels bc ON bc.blob_id = i.blob_id
JOIN channels c ON c.id = bc.channel_id
WHERE i.id = ?
ORDER BY c.priority DESC, c.id ASC;

-- name: ListBlobChannelsExport :many
SELECT blob_id, channel_id, state, attempts, last_error_reason, last_error, updated_at
FROM blob_channels
ORDER BY blob_id, channel_id;
