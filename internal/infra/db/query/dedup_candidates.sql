-- name: CreateDedupCandidate :exec
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
