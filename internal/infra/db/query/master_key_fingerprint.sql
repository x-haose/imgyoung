-- name: GetMasterKeyFingerprint :one
SELECT fingerprint
FROM master_key_fingerprint
WHERE id = 1;

-- name: InsertMasterKeyFingerprint :exec
INSERT INTO master_key_fingerprint (id, fingerprint)
VALUES (1, ?);

-- name: HasMasterKeyDependents :one
SELECT EXISTS (SELECT 1 FROM channels WHERE config_sealed <> '' OR encrypted = 1)
    OR EXISTS (SELECT 1 FROM settings WHERE cdn_cloudflare_api_token <> '')
    OR EXISTS (SELECT 1 FROM tokens) AS has_dependents;
