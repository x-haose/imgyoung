-- name: UpsertTag :one
INSERT INTO tags (user_id, name)
VALUES (?, ?)
ON CONFLICT (user_id, name) DO UPDATE SET name = excluded.name
RETURNING id;

-- name: AddImageTag :exec
INSERT OR IGNORE INTO image_tags (image_id, tag_id)
VALUES (?, ?);

-- name: ClearImageTags :exec
DELETE FROM image_tags
WHERE image_id = ?;

-- name: RemoveImageTag :exec
DELETE FROM image_tags
WHERE image_id = @image_id
  AND tag_id IN (SELECT id FROM tags WHERE user_id = @user_id AND name = @name);

-- name: CountImageTags :one
SELECT COUNT(*)
FROM image_tags
WHERE image_id = ?;

-- name: ListImageTagNames :many
SELECT t.name
FROM image_tags it
JOIN tags t ON t.id = it.tag_id
WHERE it.image_id = ?
ORDER BY t.name;

-- name: ListImageTagsExport :many
SELECT it.image_id, t.name
FROM image_tags it
JOIN tags t ON t.id = it.tag_id
ORDER BY it.image_id, t.name;

-- name: ListUserTags :many
SELECT t.name, COUNT(*) AS image_count
FROM tags t
JOIN image_tags it ON it.tag_id = t.id
WHERE t.user_id = ?
GROUP BY t.id
ORDER BY t.name;
