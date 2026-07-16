-- name: CreateToken :one
INSERT INTO tokens (user_id, name, token_hash, prefix, scopes)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: GetToken :one
SELECT *
FROM tokens
WHERE id = ?;

-- name: GetActiveTokenByHash :one
SELECT t.id, t.user_id, t.scopes, u.disabled AS user_disabled
FROM tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = ? AND t.revoked_at IS NULL;

-- name: ListTokensByUser :many
SELECT *
FROM tokens
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: RevokeToken :execrows
UPDATE tokens
SET revoked_at = ?
WHERE id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: TouchTokenUsed :exec
UPDATE tokens
SET last_used_at = ?
WHERE id = ?;
