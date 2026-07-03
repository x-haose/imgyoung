-- name: CreateAuditLog :exec
INSERT INTO audit_log (actor, action, target, detail)
VALUES (?, ?, ?, ?);
