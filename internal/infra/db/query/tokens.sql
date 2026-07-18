-- name: CreateToken :one
INSERT INTO tokens (user_id, name, token_hash, prefix, scopes, expires_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetToken :one
SELECT *
FROM tokens
WHERE id = ?;

-- name: GetActiveTokenByHash :one
SELECT t.id, t.user_id, t.scopes, u.disabled AS user_disabled
FROM tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = @token_hash
  AND t.revoked_at IS NULL
  AND (t.expires_at IS NULL OR t.expires_at > @now);

-- name: ListTokensByUser :many
SELECT *
FROM tokens
WHERE user_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountTokensByUser :one
SELECT count(*)
FROM tokens
WHERE user_id = ?;

-- name: RevokeToken :execrows
UPDATE tokens
SET revoked_at = ?
WHERE id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: RevokeTokenByName :execrows
UPDATE tokens
SET revoked_at = ?
WHERE user_id = ? AND name = ? AND revoked_at IS NULL;

-- name: RevokeExpiredTokenByName :execrows
UPDATE tokens
SET revoked_at = @revoked_at
WHERE user_id = @user_id AND name = @name AND revoked_at IS NULL
  AND expires_at IS NOT NULL AND expires_at <= @now;

-- name: DeleteTokenRecord :execrows
DELETE FROM tokens
WHERE id = @id AND user_id = @user_id
  AND (revoked_at IS NOT NULL OR (expires_at IS NOT NULL AND expires_at <= @now));

-- name: TouchTokenUsed :exec
UPDATE tokens
SET last_used_at = ?
WHERE id = ?;

-- name: DeleteTokensByUser :exec
DELETE FROM tokens
WHERE user_id = ?;

-- name: ListTokensExport :many
SELECT id, user_id, name, prefix, scopes, expires_at, last_used_at, revoked_at, created_at
FROM tokens
ORDER BY id;
