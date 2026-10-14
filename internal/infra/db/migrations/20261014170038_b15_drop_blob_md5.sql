-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_blobs" table
CREATE TABLE `new_blobs` (
  `id` integer NULL,
  `sha256` text NOT NULL,
  `phash` integer NOT NULL,
  `storage_key` text NOT NULL,
  `mime` text NOT NULL,
  `size` integer NOT NULL,
  `width` integer NOT NULL,
  `height` integer NOT NULL,
  `ref_count` integer NOT NULL DEFAULT 1,
  `pending_delete` integer NOT NULL DEFAULT 0,
  `delete_after` text NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Copy rows from old table "blobs" to new temporary table "new_blobs"
INSERT INTO `new_blobs` (`id`, `sha256`, `phash`, `storage_key`, `mime`, `size`, `width`, `height`, `ref_count`, `pending_delete`, `delete_after`, `created_at`) SELECT `id`, `sha256`, `phash`, `storage_key`, `mime`, `size`, `width`, `height`, `ref_count`, `pending_delete`, `delete_after`, `created_at` FROM `blobs`;
-- Drop "blobs" table after copying rows
DROP TABLE `blobs`;
-- Rename temporary table "new_blobs" to "blobs"
ALTER TABLE `new_blobs` RENAME TO `blobs`;
-- Create index "idx_blobs_sha256" to table: "blobs"
CREATE INDEX `idx_blobs_sha256` ON `blobs` (`sha256`);
-- Create index "idx_blobs_delete_after" to table: "blobs"
CREATE INDEX `idx_blobs_delete_after` ON `blobs` (`delete_after`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
