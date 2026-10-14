-- name: CreateImage :exec
INSERT INTO images (id, blob_id, user_id, token_id, file_name, object_key, expire_at, ttl_seconds)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetImageIDByObjectKey :one
SELECT id
FROM images
WHERE user_id = @user_id AND object_key = @object_key;

-- name: ClearObjectKey :execrows
UPDATE images
SET object_key = NULL
WHERE user_id = @user_id AND object_key = @object_key;

-- name: GetImageBlob :one
SELECT i.id, i.file_name, i.blob_id, i.created_at, b.storage_key, b.mime, b.size, b.sha256
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.id = ?;

-- name: ListExpiredImages :many
SELECT id, blob_id
FROM images
WHERE expire_at IS NOT NULL AND expire_at < ?
ORDER BY expire_at
LIMIT ?;

-- name: DeleteImage :execrows
DELETE FROM images
WHERE id = ?;

-- name: TouchImageAccess :exec
UPDATE images
SET last_public_access_at = ?, pending_cleanup = 0, access_count = access_count + 1
WHERE id = ?;

-- name: LabelColdImages :execrows
UPDATE images
SET pending_cleanup = 1
WHERE pending_cleanup = 0
  AND cleanup_exempt = 0
  AND COALESCE(last_public_access_at, created_at) < ?;

-- name: RepointImages :execrows
UPDATE images
SET blob_id = ?
WHERE blob_id = ?;

-- name: ListImagesAdmin :many
SELECT i.id, i.user_id, i.token_id, t.name AS token_name, i.file_name,
       i.expire_at, i.ttl_seconds, i.last_public_access_at, i.access_count,
       i.pending_cleanup, i.cleanup_exempt, i.created_at,
       b.mime, b.size, b.width, b.height, b.sha256, b.ref_count
FROM images i
JOIN blobs b ON b.id = i.blob_id
LEFT JOIN tokens t ON t.id = i.token_id
WHERE (@user_id = '' OR i.user_id = @user_id)
  AND (@token_id = 0 OR i.token_id = @token_id)
  AND (@search = '' OR i.id LIKE '%' || @search || '%' OR i.file_name LIKE '%' || @search || '%')
  AND (@since = '' OR i.created_at >= @since)
  AND (@until = '' OR i.created_at < @until)
  AND (@exempt = 0 OR i.cleanup_exempt = 1)
ORDER BY i.created_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountImagesAdmin :one
SELECT COUNT(*)
FROM images i
WHERE (@user_id = '' OR i.user_id = @user_id)
  AND (@token_id = 0 OR i.token_id = @token_id)
  AND (@search = '' OR i.id LIKE '%' || @search || '%' OR i.file_name LIKE '%' || @search || '%')
  AND (@since = '' OR i.created_at >= @since)
  AND (@until = '' OR i.created_at < @until)
  AND (@exempt = 0 OR i.cleanup_exempt = 1);

-- name: GetImageAdmin :one
SELECT i.id, i.user_id, i.token_id, t.name AS token_name, i.file_name,
       i.expire_at, i.ttl_seconds, i.last_public_access_at, i.access_count,
       i.pending_cleanup, i.cleanup_exempt, i.created_at,
       b.mime, b.size, b.width, b.height, b.sha256, b.ref_count
FROM images i
JOIN blobs b ON b.id = i.blob_id
LEFT JOIN tokens t ON t.id = i.token_id
WHERE i.id = ?;

-- name: GetImageOwner :one
SELECT user_id
FROM images
WHERE id = ?;

-- name: CountImagesByUser :one
SELECT COUNT(*)
FROM images
WHERE user_id = ?;

-- name: ListPendingCleanupImages :many
SELECT i.id, i.user_id, i.file_name, i.last_public_access_at, i.created_at,
       b.mime, b.size, b.width, b.height
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.pending_cleanup = 1
ORDER BY COALESCE(i.last_public_access_at, i.created_at)
LIMIT ? OFFSET ?;

-- name: CountPendingCleanupImages :one
SELECT COUNT(*)
FROM images
WHERE pending_cleanup = 1;

-- name: SetImageCleanupExempt :execrows
UPDATE images
SET cleanup_exempt = @exempt, pending_cleanup = 0
WHERE id = @id;

-- name: UpdateImageExpiry :execrows
UPDATE images
SET expire_at = @expire_at, ttl_seconds = @ttl_seconds
WHERE id = @id;

-- name: RenewImagesTTL :execrows
UPDATE images
SET expire_at = datetime(CAST(@now AS TEXT), '+' || ttl_seconds || ' seconds')
WHERE id IN (sqlc.slice('ids'))
  AND expire_at IS NOT NULL
  AND ttl_seconds IS NOT NULL;

-- name: CountImages :one
SELECT COUNT(*)
FROM images;

-- name: AccessTotals :one
SELECT CAST(COALESCE(SUM(i.access_count), 0) AS INTEGER)          AS total_accesses,
       CAST(COALESCE(SUM(i.access_count * b.size), 0) AS INTEGER) AS traffic_bytes
FROM images i
JOIN blobs b ON b.id = i.blob_id;

-- name: TopAccessedImages :many
SELECT i.id, i.file_name, i.access_count, b.size
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.access_count > 0
ORDER BY i.access_count DESC, i.id
LIMIT ?;

-- name: TotalStorageUsage :one
SELECT CAST(COALESCE(SUM(b.size), 0) AS INTEGER) AS bytes
FROM images i
JOIN blobs b ON b.id = i.blob_id;

-- name: ListImagesExport :many
SELECT id, blob_id, user_id, token_id, file_name, object_key, expire_at, ttl_seconds,
       last_public_access_at, access_count, pending_cleanup, cleanup_exempt, created_at
FROM images
ORDER BY created_at;
