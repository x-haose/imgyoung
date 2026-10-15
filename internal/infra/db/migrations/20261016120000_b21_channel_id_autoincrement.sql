-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_channels" table: AUTOINCREMENT 让删除后的主键不再复用,补齐、删除与排空任务的参数只认主键
CREATE TABLE `new_channels` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
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
  `last_error_stage` text NULL,
  `last_error_reason` text NULL,
  `last_error` text NULL,
  `template` text NOT NULL DEFAULT '',
  `pending_delete` integer NOT NULL DEFAULT 0
);
-- Copy rows from old table "channels" to new temporary table "new_channels";sqlite_sequence 随之记下现有最大主键
INSERT INTO `new_channels` (`id`, `name`, `driver`, `config_sealed`, `priority`, `healthy`, `encrypted`, `allow_direct_link`, `created_at`, `bound_at`, `last_checked_at`, `last_error_stage`, `last_error_reason`, `last_error`, `template`, `pending_delete`) SELECT `id`, `name`, `driver`, `config_sealed`, `priority`, `healthy`, `encrypted`, `allow_direct_link`, `created_at`, `bound_at`, `last_checked_at`, `last_error_stage`, `last_error_reason`, `last_error`, `template`, `pending_delete` FROM `channels`;
-- Drop "channels" table after copying rows
DROP TABLE `channels`;
-- Rename temporary table "new_channels" to "channels"
ALTER TABLE `new_channels` RENAME TO `channels`;
-- Create index "channels_name" to table: "channels"
CREATE UNIQUE INDEX `channels_name` ON `channels` (`name`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
