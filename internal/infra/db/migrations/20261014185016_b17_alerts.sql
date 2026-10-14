-- Create "alerts" table
CREATE TABLE `alerts` (
  `id` integer NULL,
  `kind` text NOT NULL,
  `target` text NOT NULL DEFAULT '',
  `message` text NOT NULL,
  `opened_at` text NOT NULL DEFAULT (datetime('now')),
  `resolved_at` text NULL,
  `open_notified` integer NOT NULL DEFAULT 0,
  `resolve_notified` integer NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`)
);
-- Create index "idx_alerts_open" to table: "alerts"
CREATE UNIQUE INDEX `idx_alerts_open` ON `alerts` (`kind`, `target`) WHERE resolved_at IS NULL;
-- Create index "idx_alerts_opened_at" to table: "alerts"
CREATE INDEX `idx_alerts_opened_at` ON `alerts` (`opened_at`);
