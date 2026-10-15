-- Create "master_key_fingerprint" table
CREATE TABLE `master_key_fingerprint` (
  `id` integer NULL,
  `fingerprint` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CHECK (id = 1)
);
