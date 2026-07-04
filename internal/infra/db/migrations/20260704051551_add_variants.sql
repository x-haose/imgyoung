-- Create "variants" table
CREATE TABLE `variants` (
  `id` integer NULL,
  `blob_id` integer NOT NULL,
  `params_hash` text NOT NULL,
  `format` text NOT NULL,
  `size` integer NOT NULL,
  `last_access` text NOT NULL DEFAULT (datetime('now')),
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`blob_id`) REFERENCES `blobs` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_variants_key" to table: "variants"
CREATE UNIQUE INDEX `idx_variants_key` ON `variants` (`blob_id`, `params_hash`);
-- Create index "idx_variants_last_access" to table: "variants"
CREATE INDEX `idx_variants_last_access` ON `variants` (`last_access`);
-- Create index "idx_variants_blob" to table: "variants"
CREATE INDEX `idx_variants_blob` ON `variants` (`blob_id`);
