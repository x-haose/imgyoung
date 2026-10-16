-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (user_id, key, image_id)
VALUES (?, ?, ?);

-- name: ReleaseStaleIdempotencyKey :exec
DELETE FROM idempotency_keys
WHERE idempotency_keys.user_id = @user_id AND idempotency_keys.key = @key
  AND (idempotency_keys.created_at < CAST(@since AS TEXT)
       OR EXISTS (SELECT 1 FROM images i
                  WHERE i.id = idempotency_keys.image_id AND i.expire_at <= CAST(@now AS TEXT)));

-- name: GetIdempotencyImage :one
SELECT k.image_id
FROM idempotency_keys k
JOIN images i ON i.id = k.image_id
WHERE k.user_id = @user_id AND k.key = @key AND k.created_at >= CAST(@since AS TEXT)
  AND (i.expire_at IS NULL OR i.expire_at > CAST(@now AS TEXT));

-- name: DeleteIdempotencyKeysByImage :exec
DELETE FROM idempotency_keys
WHERE image_id = ?;

-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys
WHERE created_at < ?;
