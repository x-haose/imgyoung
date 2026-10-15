-- name: GetMasterKeyFingerprint :one
SELECT fingerprint
FROM master_key_fingerprint
WHERE id = 1;

-- name: InsertMasterKeyFingerprint :exec
INSERT INTO master_key_fingerprint (id, fingerprint)
VALUES (1, ?);

-- name: FirstSealedValue :one
SELECT sealed
FROM (SELECT config_sealed AS sealed FROM channels WHERE config_sealed <> ''
      UNION ALL
      SELECT cdn_cloudflare_api_token AS sealed FROM settings WHERE cdn_cloudflare_api_token <> '')
LIMIT 1;
