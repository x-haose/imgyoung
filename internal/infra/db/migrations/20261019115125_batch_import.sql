-- Create "import_item_sources" table
CREATE TABLE `import_item_sources` (
  `id` integer NULL,
  `item_id` integer NOT NULL,
  `source` text NOT NULL,
  `raw` text NOT NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`item_id`) REFERENCES `import_items` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "idx_import_item_sources_ref" to table: "import_item_sources"
CREATE UNIQUE INDEX `idx_import_item_sources_ref` ON `import_item_sources` (`item_id`, `source`, `raw`);
-- Create "import_items" table
CREATE TABLE `import_items` (
  `id` integer NULL,
  `job_id` text NOT NULL,
  `url` text NOT NULL,
  `host` text NOT NULL,
  `state` text NOT NULL,
  `image_id` text NULL,
  `error_code` text NOT NULL DEFAULT '',
  `error_detail` text NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`image_id`) REFERENCES `images` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `1` FOREIGN KEY (`job_id`) REFERENCES `import_jobs` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "idx_import_items_job_url" to table: "import_items"
CREATE UNIQUE INDEX `idx_import_items_job_url` ON `import_items` (`job_id`, `url`);
-- Create index "idx_import_items_job_state" to table: "import_items"
CREATE INDEX `idx_import_items_job_state` ON `import_items` (`job_id`, `state`, `id`);
-- Create index "idx_import_items_job_host" to table: "import_items"
CREATE INDEX `idx_import_items_job_host` ON `import_items` (`job_id`, `host`, `state`);
-- Create index "idx_import_items_image" to table: "import_items"
CREATE INDEX `idx_import_items_image` ON `import_items` (`image_id`);
-- Create "import_jobs" table
CREATE TABLE `import_jobs` (
  `id` text NULL,
  `user_id` text NOT NULL,
  `mode` text NOT NULL,
  `status` text NOT NULL,
  `seeds` text NOT NULL DEFAULT '[]',
  `ttl_seconds` integer NULL,
  `exif_policy` text NOT NULL DEFAULT '',
  `referer` text NOT NULL DEFAULT '',
  `scan_pages_found` integer NOT NULL DEFAULT 0,
  `scan_pages_done` integer NOT NULL DEFAULT 0,
  `scan_report` text NULL,
  `error_code` text NOT NULL DEFAULT '',
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  `finished_at` text NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "idx_import_jobs_user" to table: "import_jobs"
CREATE INDEX `idx_import_jobs_user` ON `import_jobs` (`user_id`, `created_at`);
-- Create index "idx_import_jobs_status" to table: "import_jobs"
CREATE INDEX `idx_import_jobs_status` ON `import_jobs` (`status`, `created_at`);
