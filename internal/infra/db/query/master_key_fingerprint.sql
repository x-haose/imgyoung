-- name: GetMasterKeyFingerprint :one
SELECT fingerprint
FROM master_key_fingerprint
WHERE id = 1;

-- name: InsertMasterKeyFingerprint :exec
INSERT INTO master_key_fingerprint (id, fingerprint)
VALUES (1, ?);
