-- Add column "must_change_password" to table: "users"
ALTER TABLE `users` ADD COLUMN `must_change_password` integer NOT NULL DEFAULT 0;
