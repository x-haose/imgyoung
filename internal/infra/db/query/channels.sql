-- name: CreateChannel :one
INSERT INTO channels (name, driver, config_sealed, priority, encrypted, allow_direct_link, template)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ListChannels :many
SELECT *
FROM channels
ORDER BY priority DESC, id ASC;

-- name: GetChannel :one
SELECT *
FROM channels
WHERE id = ?;

-- name: UpdateChannelProbeResult :exec
UPDATE channels
SET healthy         = ?,
    last_checked_at = datetime('now'),
    last_error      = ?
WHERE id = ?;

-- name: UpdateChannelConfig :exec
UPDATE channels
SET name              = ?,
    driver            = ?,
    config_sealed     = ?,
    priority          = ?,
    allow_direct_link = ?,
    template          = ?
WHERE id = ?;

-- name: MarkChannelPendingDelete :exec
UPDATE channels
SET pending_delete = 1
WHERE id = ?;

-- name: UpdateChannelConfigSealed :exec
UPDATE channels
SET config_sealed = ?
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
