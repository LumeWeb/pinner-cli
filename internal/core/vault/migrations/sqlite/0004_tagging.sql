-- +goose Up

-- First-class vault tagging (P1, the backbone of search).
--
-- Two tables model normalized tags so they can be indexed, listed MRU, and
-- reconciled against the durable Sia object metadata:
--
--   * `tags`      — one row per distinct tag name. `used_at` is bumped on every
--                   tag application so vault_tag_ls can return tags in
--                   most-recently-used order (MRU). A tag with zero remaining
--                   file_tags links is pruned (see the re-pin reconcile path),
--                   so `tags` never holds dead rows.
--   * `file_tags` — the join between a materialized file row (by its local id)
--                   and a tag. The primary key (file_id, tag_id) gives each
--                   (file, tag) pairing exactly once. `idx_file_tags_tag`
--                   makes tag -> files lookups cheap (search backstop).
--
-- DURABILITY RULE: the local join is a CACHE of the authoritative tags that
-- live in the Sia object's sealed FileMetadata under Metadata['tags'] as a
-- planar []string. Every durable tag mutation goes through the re-pin-and-write
-- path (re-read object -> merge -> re-encode -> UpdateMetadata -> PinObject ->
-- then reconcile the local join in one transaction). Sync-down reconciles the
-- join from the object's Metadata['tags'] array in the same transaction, so a
-- cache rebuild can never clobber remote tags.

CREATE TABLE IF NOT EXISTS `tags`
(
    `id`         integer PRIMARY KEY AUTOINCREMENT,
    `name`       text    NOT NULL,
    `created_at` datetime NOT NULL,
    `used_at`    datetime NOT NULL
);

-- Tag names are case-insensitively unique (normalized to lowercase on write).
CREATE UNIQUE INDEX IF NOT EXISTS `idx_tags_name` ON `tags`(`name` COLLATE NOCASE);

-- MRU listing: most-recently-used tag first.
CREATE INDEX IF NOT EXISTS `idx_tags_used_at` ON `tags`(`used_at` DESC);

CREATE TABLE IF NOT EXISTS `file_tags`
(
    `file_id`    integer NOT NULL,
    `tag_id`     integer NOT NULL,
    `created_at` datetime NOT NULL,
    PRIMARY KEY (`file_id`, `tag_id`),
    CONSTRAINT `fk_file_tags_file` FOREIGN KEY (`file_id`) REFERENCES `files`(`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_file_tags_tag` FOREIGN KEY (`tag_id`) REFERENCES `tags`(`id`) ON DELETE CASCADE
);

-- Tag -> files lookups (search backstop).
CREATE INDEX IF NOT EXISTS `idx_file_tags_tag` ON `file_tags`(`tag_id`);

-- +goose Down

DROP INDEX IF EXISTS `idx_file_tags_tag`;
DROP TABLE IF EXISTS `file_tags`;
DROP INDEX IF EXISTS `idx_tags_used_at`;
DROP INDEX IF EXISTS `idx_tags_name`;
DROP TABLE IF EXISTS `tags`;
