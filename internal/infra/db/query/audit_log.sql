-- name: CreateAuditLog :exec
INSERT INTO audit_log (actor, action, target, detail)
VALUES (?, ?, ?, ?);

-- name: ListAuditLog :many
SELECT a.id, a.actor, a.action, a.target, a.detail, a.created_at,
       u.name AS actor_name
FROM audit_log a
LEFT JOIN users u ON u.id = a.actor
WHERE (@actor = '' OR a.actor = @actor)
  AND (@action = '' OR a.action = @action)
  AND (@target = '' OR a.target = @target)
  AND (@since = '' OR a.created_at >= @since)
  AND (@until = '' OR a.created_at < @until)
ORDER BY a.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountAuditLog :one
SELECT COUNT(*)
FROM audit_log
WHERE (@actor = '' OR actor = @actor)
  AND (@action = '' OR action = @action)
  AND (@target = '' OR target = @target)
  AND (@since = '' OR created_at >= @since)
  AND (@until = '' OR created_at < @until);
