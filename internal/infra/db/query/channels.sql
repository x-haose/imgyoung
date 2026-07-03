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

-- name: ChannelStorageUsage :many
SELECT c.id, c.name, CAST(COALESCE(SUM(b.size), 0) AS INTEGER) AS bytes
FROM channels c
LEFT JOIN blob_channels bc ON bc.channel_id = c.id AND bc.state = 'done'
LEFT JOIN blobs b ON b.id = bc.blob_id
GROUP BY c.id, c.name;
