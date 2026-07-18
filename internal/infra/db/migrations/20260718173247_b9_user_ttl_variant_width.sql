-- Add column "default_ttl_hours" to table: "users"
ALTER TABLE `users` ADD COLUMN `default_ttl_hours` integer NULL;
-- Add column "width" to table: "variants"
ALTER TABLE `variants` ADD COLUMN `width` integer NOT NULL DEFAULT 0;
