-- name: CreateImage :exec
INSERT INTO images (id, storage_key, mime, size)
VALUES (?, ?, ?, ?);

-- name: GetImage :one
SELECT *
FROM images
WHERE id = ?;
