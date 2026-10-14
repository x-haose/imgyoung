-- name: GetVariant :one
SELECT *
FROM variants
WHERE blob_id = ? AND params_hash = ?;

-- name: UpsertVariant :exec
INSERT INTO variants (blob_id, params_hash, width, format, size)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (blob_id, params_hash) DO UPDATE SET
    width = excluded.width,
    size = excluded.size,
    last_access = datetime('now');

-- name: TouchVariants :exec
UPDATE variants
SET last_access = datetime('now')
WHERE id IN (sqlc.slice('ids'));

-- name: SumVariantsSize :one
SELECT CAST(COALESCE(SUM(size), 0) AS INTEGER)
FROM variants;

-- name: ListVariantsLRU :many
SELECT id, blob_id, params_hash, size
FROM variants
ORDER BY last_access ASC
LIMIT ?;

-- name: ListVariantsByBlob :many
SELECT id, params_hash
FROM variants
WHERE blob_id = ?;

-- name: DeleteVariant :exec
DELETE FROM variants
WHERE id = ?;

-- name: DeleteVariantsByBlob :exec
DELETE FROM variants
WHERE blob_id = ?;

-- name: ListVariantURLParams :many
SELECT DISTINCT width, format
FROM variants
WHERE blob_id = ?;
