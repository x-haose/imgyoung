-- Add column "alert_webhook_url" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `alert_webhook_url` text NOT NULL DEFAULT '';
-- Add column "audit_retention_days" to table: "settings"
ALTER TABLE `settings` ADD COLUMN `audit_retention_days` integer NOT NULL DEFAULT 0;
