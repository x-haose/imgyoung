-- name: CreateBlob :one
INSERT INTO blobs (sha256, phash, storage_key, mime, size, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: CreateImage :exec
INSERT INTO images (id, blob_id, file_name)
VALUES (?, ?, ?);

-- name: GetImageBlob :one
SELECT i.id, i.file_name, b.storage_key, b.mime, b.size
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.id = ?;

-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys (key, image_id)
VALUES (?, ?);

-- name: GetIdempotencyImage :one
SELECT image_id
FROM idempotency_keys
WHERE key = ?;
