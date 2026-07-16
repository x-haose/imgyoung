-- name: CreateDedupCandidate :execrows
INSERT OR IGNORE INTO dedup_candidates (blob_a, blob_b, distance)
VALUES (?, ?, ?);

-- name: GetDedupCandidate :one
SELECT *
FROM dedup_candidates
WHERE id = ?;

-- name: ListPendingDedupCandidates :many
SELECT *
FROM dedup_candidates
WHERE status = 'pending'
ORDER BY distance, id
LIMIT ? OFFSET ?;

-- name: ListPendingDedupCandidateDetails :many
SELECT dc.id, dc.distance, dc.created_at,
       a.id AS blob_a, a.mime AS mime_a, a.size AS size_a, a.width AS width_a,
       a.height AS height_a, a.ref_count AS refs_a,
       CAST(COALESCE((SELECT i.id FROM images i WHERE i.blob_id = a.id LIMIT 1), '') AS TEXT) AS image_a,
       b.id AS blob_b, b.mime AS mime_b, b.size AS size_b, b.width AS width_b,
       b.height AS height_b, b.ref_count AS refs_b,
       CAST(COALESCE((SELECT i.id FROM images i WHERE i.blob_id = b.id LIMIT 1), '') AS TEXT) AS image_b
FROM dedup_candidates dc
JOIN blobs a ON a.id = dc.blob_a
JOIN blobs b ON b.id = dc.blob_b
WHERE dc.status = 'pending'
ORDER BY dc.distance, dc.id
LIMIT ? OFFSET ?;

-- name: CountPendingDedupCandidates :one
SELECT COUNT(*)
FROM dedup_candidates
WHERE status = 'pending';

-- name: SetDedupCandidateStatus :execrows
UPDATE dedup_candidates
SET status = ?
WHERE id = ? AND status = 'pending';

-- name: DeleteDedupCandidatesByBlob :exec
DELETE FROM dedup_candidates
WHERE blob_a = ? OR blob_b = ?;
