-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (key, image_id)
VALUES (?, ?);

-- name: GetIdempotencyImage :one
SELECT image_id
FROM idempotency_keys
WHERE key = ?;

-- name: DeleteIdempotencyKeysByImage :exec
DELETE FROM idempotency_keys
WHERE image_id = ?;
