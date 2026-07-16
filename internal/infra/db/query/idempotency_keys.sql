-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (user_id, key, image_id)
VALUES (?, ?, ?);

-- name: GetIdempotencyImage :one
SELECT image_id
FROM idempotency_keys
WHERE user_id = ? AND key = ?;

-- name: DeleteIdempotencyKeysByImage :exec
DELETE FROM idempotency_keys
WHERE image_id = ?;
