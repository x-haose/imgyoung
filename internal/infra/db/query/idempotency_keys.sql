-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (user_id, key, image_id)
VALUES (?, ?, ?);

-- name: ReleaseExpiredIdempotencyKey :exec
DELETE FROM idempotency_keys
WHERE user_id = @user_id AND key = @key AND created_at < CAST(@since AS TEXT);

-- name: GetIdempotencyImage :one
SELECT image_id
FROM idempotency_keys
WHERE user_id = @user_id AND key = @key AND created_at >= CAST(@since AS TEXT);

-- name: DeleteIdempotencyKeysByImage :exec
DELETE FROM idempotency_keys
WHERE image_id = ?;

-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys
WHERE created_at < ?;
