-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at)
VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT s.token_hash, s.user_id, s.csrf_token, s.expires_at,
       u.name AS user_name, u.role AS user_role, u.disabled AS user_disabled,
       u.must_change_password AS user_must_change_password
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = ?;

-- name: DeleteSessionsByUser :exec
DELETE FROM sessions
WHERE user_id = ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at < ?;
