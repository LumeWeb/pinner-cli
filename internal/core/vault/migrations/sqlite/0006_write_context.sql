-- +goose Up

-- Normalized write-context columns (P-searchable projection of metadata).
--
-- The encapsulated object metadata carries opaque write-context KV (src, host,
-- agent) inside FileMetadata.Metadata. Those keys describe WHICH frontend
-- handled the write (src=mcp|cli), WHICH host platform (host, e.g. claude-desktop)
-- and WHICH creator agent (agent). They are conceptually separate from tags and,
-- like tags, are a CACHE: the durable source is the object's sealed metadata,
-- reconciled here on Put and on sync-down (see write_context.go). We project the
-- well-known keys into indexed columns so vault_search can filter on them without
-- a full JSON scan. Arbitrary caller KV (kind, project, role, ...) stays in the
-- JSON blob and is not projected.
--
-- No `profile` column: each profile owns its own SQLite DB, so every row already
-- belongs to that profile; a column would be constant within the DB.

ALTER TABLE `files` ADD COLUMN `source` text;
ALTER TABLE `files` ADD COLUMN `host`   text;
ALTER TABLE `files` ADD COLUMN `agent`  text;

-- Group/filter by frontend (mcp vs cli) and by host platform.
CREATE INDEX IF NOT EXISTS `idx_files_source` ON `files`(`source`);
CREATE INDEX IF NOT EXISTS `idx_files_host`   ON `files`(`host`);

-- +goose Down

-- Drop the searchability indexes but leave the columns in place, mirroring
-- 0003/0002's down convention: SQLite DROP COLUMN requires a full table
-- rebuild and SQLite >= 3.35, so leaving harmless write-context columns
-- behind keeps `goose down` a safe no-op on any SQLite version.
DROP INDEX IF EXISTS `idx_files_source`;
DROP INDEX IF EXISTS `idx_files_host`;
