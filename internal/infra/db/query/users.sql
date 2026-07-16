-- name: CreateUser :exec
INSERT INTO users (id, name, password_hash, role, quota_bytes)
VALUES (?, ?, ?, ?, ?);

-- name: GetUser :one
SELECT *
FROM users
WHERE id = ?;

-- name: GetUserByName :one
SELECT *
FROM users
WHERE name = ?;

-- name: ListUsersWithUsage :many
SELECT u.*,
       CAST(COALESCE(SUM(b.size), 0) AS INTEGER) AS used_bytes,
       CAST(COUNT(i.id) AS INTEGER) AS image_count
FROM users u
LEFT JOIN images i ON i.user_id = u.id
LEFT JOIN blobs b ON b.id = i.blob_id
GROUP BY u.id
ORDER BY u.created_at;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = ?
WHERE id = ?;

-- name: UpdateUser :exec
UPDATE users
SET name = ?, role = ?, quota_bytes = ?, disabled = ?
WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*)
FROM users;

-- name: UserStorageUsage :one
SELECT CAST(COALESCE(SUM(b.size), 0) AS INTEGER) AS bytes
FROM images i
JOIN blobs b ON b.id = i.blob_id
WHERE i.user_id = ?;
