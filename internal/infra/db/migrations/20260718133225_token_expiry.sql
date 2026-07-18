-- Add column "expires_at" to table: "tokens"
ALTER TABLE `tokens` ADD COLUMN `expires_at` text NULL;
