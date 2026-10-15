-- name: ListOpenAlerts :many
SELECT *
FROM alerts
WHERE resolved_at IS NULL
ORDER BY opened_at DESC, id DESC;

-- name: ListAlertsPage :many
SELECT *
FROM alerts
ORDER BY id DESC
LIMIT ? OFFSET ?;

-- name: CountAlerts :one
SELECT COUNT(*)
FROM alerts;

-- name: OpenAlert :one
INSERT INTO alerts (kind, target, message, params)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: UpdateAlertDescription :exec
UPDATE alerts
SET message = ?,
    params  = ?
WHERE id = ?;

-- name: ResolveAlert :exec
UPDATE alerts
SET resolved_at = datetime('now')
WHERE id = ? AND resolved_at IS NULL;

-- name: ListUnnotifiedAlerts :many
SELECT *
FROM alerts
WHERE (resolved_at IS NULL AND open_notified = 0)
   OR (resolved_at IS NOT NULL AND (open_notified = 0 OR resolve_notified = 0))
ORDER BY id;

-- name: MarkAlertOpenNotified :exec
UPDATE alerts
SET open_notified = 1
WHERE id = ?;

-- name: MarkAlertResolveNotified :exec
UPDATE alerts
SET resolve_notified = 1
WHERE id = ?;
