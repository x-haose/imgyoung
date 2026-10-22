-- Add column "tags" to table: "import_jobs"
ALTER TABLE `import_jobs` ADD COLUMN `tags` text NOT NULL DEFAULT '[]';
-- Create "image_tags" table
CREATE TABLE `image_tags` (
  `image_id` text NOT NULL,
  `tag_id` integer NOT NULL,
  PRIMARY KEY (`image_id`, `tag_id`),
  CONSTRAINT `0` FOREIGN KEY (`tag_id`) REFERENCES `tags` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT `1` FOREIGN KEY (`image_id`) REFERENCES `images` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "idx_image_tags_tag" to table: "image_tags"
CREATE INDEX `idx_image_tags_tag` ON `image_tags` (`tag_id`);
-- Create "tags" table
CREATE TABLE `tags` (
  `id` integer NULL,
  `user_id` text NOT NULL,
  `name` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (`id`),
  CONSTRAINT `0` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "idx_tags_user_name" to table: "tags"
CREATE UNIQUE INDEX `idx_tags_user_name` ON `tags` (`user_id`, `name`);
