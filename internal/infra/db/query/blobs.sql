-- name: CreateBlob :one
INSERT INTO blobs (sha256, phash, storage_key, mime, size, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetBlob :one
SELECT *
FROM blobs
WHERE id = ?;

-- name: DecrementBlobRef :one
UPDATE blobs
SET ref_count = ref_count - 1
WHERE id = ?
RETURNING ref_count;

-- name: MarkBlobForDelete :exec
UPDATE blobs
SET pending_delete = 1, delete_after = ?
WHERE id = ? AND ref_count <= 0;

-- name: ListBlobsToDelete :many
SELECT id, storage_key
FROM blobs
WHERE pending_delete = 1 AND delete_after IS NOT NULL AND delete_after <= ?
LIMIT ?;

-- name: DeleteBlob :execrows
DELETE FROM blobs
WHERE id = ? AND ref_count <= 0 AND pending_delete = 1;

-- name: ListDuplicateSha256 :many
SELECT sha256
FROM blobs
WHERE pending_delete = 0
GROUP BY sha256
HAVING COUNT(*) > 1;

-- name: ListBlobsBySha256 :many
SELECT id, ref_count
FROM blobs
WHERE sha256 = ? AND pending_delete = 0
ORDER BY id;

-- name: AddBlobRef :exec
UPDATE blobs
SET ref_count = ref_count + ?
WHERE id = ?;

-- name: MarkBlobMergedAway :exec
UPDATE blobs
SET ref_count = 0, pending_delete = 1, delete_after = ?
WHERE id = ?;

-- name: ListBlobPhashes :many
SELECT id, phash
FROM blobs
WHERE pending_delete = 0 AND id > ?
ORDER BY id;

-- name: ListBlobsExport :many
SELECT id, sha256, phash, storage_key, mime, size, width, height,
       ref_count, pending_delete, delete_after, created_at
FROM blobs
ORDER BY id;
