-- name: CreateAuditLog :exec
INSERT INTO audit_log (actor, action, target, detail)
VALUES (?, ?, ?, ?);

-- name: ListAuditLog :many
SELECT *
FROM audit_log
WHERE (@actor = '' OR actor = @actor)
  AND (@action = '' OR action = @action)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountAuditLog :one
SELECT COUNT(*)
FROM audit_log
WHERE (@actor = '' OR actor = @actor)
  AND (@action = '' OR action = @action);
