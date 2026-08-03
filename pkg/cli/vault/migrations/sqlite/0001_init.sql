-- +goose Up

-- Vault local cache schema.
-- Directories: a vault directory with a materialized path.
CREATE TABLE IF NOT EXISTS `directories`
(
    `id`         integer PRIMARY KEY AUTOINCREMENT,
    `path`       text    NOT NULL,
    `created_at` datetime NOT NULL,
    `sort_key`   text
);
CREATE UNIQUE INDEX IF NOT EXISTS `idx_directories_path` ON `directories`(`path`);

-- Files: identity is (name, directory_id). ObjectKey is the Sia object ID
-- (content-addressed hash of slabs), stored as hex.
CREATE TABLE IF NOT EXISTS `files`
(
    `id`             integer PRIMARY KEY AUTOINCREMENT,
    `name`           text    NOT NULL,
    `directory_id`   integer,
    `object_key`     text    NOT NULL,
    `size`           integer NOT NULL,
    `media_type`     text,
    `content_digest` text    NOT NULL,
    `metadata`       JSON,
    `created_at`     datetime NOT NULL,
    `updated_at`     datetime NOT NULL,
    CONSTRAINT `fk_files_directory` FOREIGN KEY (`directory_id`) REFERENCES `directories`(`id`)
);
CREATE INDEX IF NOT EXISTS `idx_files_directory_id` ON `files`(`directory_id`);
-- Composite unique index for (name, directory_id). COALESCE handles NULL FK
-- (root directory) so root and directory files share one uniqueness space.
CREATE UNIQUE INDEX IF NOT EXISTS `idx_files_name_dir` ON `files`(`name`, COALESCE(`directory_id`, 0));

-- Sync-down cursor: the indexer event cursor for incremental sync.
CREATE TABLE IF NOT EXISTS `sync_down_cursors`
(
    `id`           integer PRIMARY KEY AUTOINCREMENT,
    `cursor`       text,
    `pending_skip` numeric,
    `updated_at`   datetime
);

-- +goose Down

DROP TABLE IF EXISTS `sync_down_cursors`;
DROP TABLE IF EXISTS `files`;
DROP TABLE IF EXISTS `directories`;
