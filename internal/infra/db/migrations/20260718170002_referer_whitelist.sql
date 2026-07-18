-- Create "referer_whitelist" table
CREATE TABLE `referer_whitelist` (
  `id` integer NULL,
  `pattern` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`)
);
-- Create index "referer_whitelist_pattern" to table: "referer_whitelist"
CREATE UNIQUE INDEX `referer_whitelist_pattern` ON `referer_whitelist` (`pattern`);
