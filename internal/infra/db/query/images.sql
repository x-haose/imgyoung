-- name: CreateImage :exec
INSERT INTO images (id, blob_id, user_id, file_name, expire_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetImageBlob :one
SELECT i.id, i.file_name, i.blob_id, b.storage_key, b.mime, b.size, b.sha256
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.id = ?;

-- name: ListExpiredImages :many
SELECT id, blob_id
FROM images
WHERE expire_at IS NOT NULL AND expire_at < ?
ORDER BY expire_at
LIMIT ?;

-- name: DeleteImage :exec
DELETE FROM images
WHERE id = ?;

-- name: TouchImageAccess :exec
UPDATE images
SET last_public_access_at = ?, pending_cleanup = 0
WHERE id = ?;

-- name: LabelColdImages :execrows
UPDATE images
SET pending_cleanup = 1
WHERE pending_cleanup = 0
  AND COALESCE(last_public_access_at, created_at) < ?;

-- name: RepointImages :exec
UPDATE images
SET blob_id = ?
WHERE blob_id = ?;
