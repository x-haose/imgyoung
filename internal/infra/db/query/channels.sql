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

-- name: UpdateChannelProbeResult :execrows
UPDATE channels
SET healthy           = ?,
    last_checked_at   = datetime('now'),
    last_error_stage  = ?,
    last_error_reason = ?,
    last_error        = ?
WHERE id = ? AND pending_delete = 0;

-- name: RecordChannelLoadFailure :exec
UPDATE channels
SET healthy           = 0,
    last_checked_at   = datetime('now'),
    last_error_stage  = 'load',
    last_error_reason = ?,
    last_error        = ?
WHERE id = ?;

-- name: RecordChannelDrainFailure :execrows
UPDATE channels
SET healthy           = 0,
    last_error_stage  = 'drain',
    last_error_reason = ?,
    last_error        = ?
WHERE id = ? AND pending_delete = 1;

-- name: UpdateChannelConfig :execrows
UPDATE channels
SET name              = sqlc.arg(name),
    driver            = sqlc.arg(driver),
    config_sealed     = sqlc.arg(config_sealed),
    priority          = sqlc.arg(priority),
    allow_direct_link = sqlc.arg(allow_direct_link),
    template          = sqlc.arg(template),
    bound_at          = sqlc.arg(bound_at)
WHERE id = sqlc.arg(id) AND pending_delete = 0 AND config_sealed = sqlc.arg(expected_sealed);

-- name: RebindAllChannels :execrows
UPDATE channels
SET bound_at = datetime('now');

-- name: MarkChannelPendingDelete :exec
UPDATE channels
SET pending_delete    = 1,
    last_error_stage  = NULL,
    last_error_reason = NULL,
    last_error        = NULL
WHERE id = ? AND pending_delete = 0;

-- name: ClearChannelLoadFailure :exec
UPDATE channels
SET last_error_stage  = NULL,
    last_error_reason = NULL,
    last_error        = NULL
WHERE id = ? AND last_error_stage = 'load';

-- name: UpdateChannelConfigSealed :execrows
UPDATE channels
SET config_sealed = sqlc.arg(config_sealed)
WHERE id = sqlc.arg(id) AND config_sealed = sqlc.arg(expected_sealed);

-- name: DeleteChannel :exec
DELETE FROM channels
WHERE id = ?;

-- name: ChannelStorageUsage :many
SELECT c.id, c.name, CAST(COALESCE(SUM(b.size), 0) AS INTEGER) AS bytes
FROM channels c
LEFT JOIN blob_channels bc ON bc.channel_id = c.id AND bc.state = 'done'
LEFT JOIN blobs b ON b.id = bc.blob_id
GROUP BY c.id, c.name;
