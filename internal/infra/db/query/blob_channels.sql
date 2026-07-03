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

-- name: ListBlobChannelIDs :many
SELECT channel_id
FROM blob_channels
WHERE blob_id = ?;

-- name: DeleteBlobChannelsByBlob :exec
DELETE FROM blob_channels
WHERE blob_id = ?;
