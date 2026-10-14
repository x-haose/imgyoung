-- Add column "idempotency_key_ttl_hours" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `idempotency_key_ttl_hours` integer NOT NULL DEFAULT 24;
