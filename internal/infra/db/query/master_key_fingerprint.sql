-- name: GetMasterKeyFingerprint :one
SELECT fingerprint
FROM master_key_fingerprint
WHERE id = 1;

-- name: InsertMasterKeyFingerprint :exec
INSERT INTO master_key_fingerprint (id, fingerprint)
VALUES (1, ?);

-- name: ListSealedValues :many
SELECT config_sealed AS sealed FROM channels WHERE config_sealed <> ''
UNION ALL
SELECT cdn_cloudflare_api_token AS sealed FROM settings WHERE cdn_cloudflare_api_token <> '';

-- name: HasMasterKeyDerivedData :one
SELECT EXISTS (SELECT 1 FROM tokens) OR EXISTS (SELECT 1 FROM channels WHERE encrypted = 1) AS has_derived;
