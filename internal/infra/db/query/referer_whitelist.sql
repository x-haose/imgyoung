-- name: ListRefererWhitelist :many
SELECT *
FROM referer_whitelist
ORDER BY id;

-- name: CreateRefererPattern :one
INSERT INTO referer_whitelist (pattern)
VALUES (?)
RETURNING *;

-- name: DeleteRefererPattern :one
DELETE FROM referer_whitelist
WHERE id = ?
RETURNING pattern;

-- name: CountRefererWhitelist :one
SELECT COUNT(*)
FROM referer_whitelist;

-- name: SeedRefererPattern :exec
INSERT OR IGNORE INTO referer_whitelist (pattern)
VALUES (?);
