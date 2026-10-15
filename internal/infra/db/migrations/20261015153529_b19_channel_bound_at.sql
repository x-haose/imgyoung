-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_channels" table
CREATE TABLE `new_channels` (
  `id` integer NULL,
  `name` text NOT NULL,
  `driver` text NOT NULL,
  `config_sealed` text NOT NULL DEFAULT '',
  `priority` integer NOT NULL DEFAULT 100,
  `healthy` integer NOT NULL DEFAULT 1,
  `encrypted` integer NOT NULL DEFAULT 0,
  `allow_direct_link` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  `bound_at` text NOT NULL DEFAULT (datetime('now')),
  `last_checked_at` text NULL,
  `last_error` text NULL,
  `template` text NOT NULL DEFAULT '',
  `pending_delete` integer NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`)
);
-- Copy rows from old table "channels" to new temporary table "new_channels"
-- Backfill: 存量渠道的绑定时刻取各自的创建时刻
INSERT INTO `new_channels` (`id`, `name`, `driver`, `config_sealed`, `priority`, `healthy`, `encrypted`, `allow_direct_link`, `created_at`, `bound_at`, `last_checked_at`, `last_error`, `template`, `pending_delete`) SELECT `id`, `name`, `driver`, `config_sealed`, `priority`, `healthy`, `encrypted`, `allow_direct_link`, `created_at`, `created_at`, `last_checked_at`, `last_error`, `template`, `pending_delete` FROM `channels`;
-- Drop "channels" table after copying rows
DROP TABLE `channels`;
-- Rename temporary table "new_channels" to "channels"
ALTER TABLE `new_channels` RENAME TO `channels`;
-- Create index "channels_name" to table: "channels"
CREATE UNIQUE INDEX `channels_name` ON `channels` (`name`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
