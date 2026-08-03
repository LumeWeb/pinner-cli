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

-- Files: identity is a stable per-file UUID (the same UUID carried in the Sia
-- object's encrypted metadata), NOT the name. `name` is intentionally
-- non-unique: two distinct content-addressed objects may legitimately carry the
-- same user-facing name (different sources), and both must remain visible in
-- the vault rather than one being silently dropped. Sync maps events by UUID,
-- so a rename is just an update of `name` on the same row and never collides.
CREATE TABLE IF NOT EXISTS `files`
(
    `id`             integer PRIMARY KEY AUTOINCREMENT,
    `uuid`           text    NOT NULL,       -- stable per-file identity, from object metadata
    `name`           text    NOT NULL,
    `directory_id`   integer,
    `object_key`     text    NOT NULL,       -- Sia object ID (hex of the content hash)
    `size`           integer NOT NULL,
    `media_type`     text,
    `content_digest` text    NOT NULL,
    `metadata`       JSON,
    `deleted_at`     datetime,               -- tombstone (soft delete); NULL = live
    `created_at`     datetime NOT NULL,
    `updated_at`     datetime NOT NULL,
    CONSTRAINT `fk_files_directory` FOREIGN KEY (`directory_id`) REFERENCES `directories`(`id`) ON DELETE SET NULL
);

-- Stable identity lookup (sync, delete-by-object).
CREATE UNIQUE INDEX IF NOT EXISTS `idx_files_uuid` ON `files`(`uuid`);

-- Hot sync lookup: every event batch matches objects by their content key.
CREATE INDEX IF NOT EXISTS `idx_files_object_key` ON `files`(`object_key`);

-- Navigation: list files in a directory.
CREATE INDEX IF NOT EXISTS `idx_files_directory_id` ON `files`(`directory_id`);

-- "Current file per (name, directory)" display rule. Intentionally NON-unique:
-- this groups the rows that share a name/dir and lets the app pick the current
-- winner (updated_at DESC, id DESC) without ever dropping a distinct object.
CREATE INDEX IF NOT EXISTS `idx_files_name_dir` ON `files`(`name`, COALESCE(`directory_id`, 0));

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
