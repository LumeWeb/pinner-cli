-- +goose Up

-- Vault versioning: each overwrite of a path inserts a NEW version row instead of
-- mutating the existing row in place, so prior content is preserved and retrievable.
-- This mirrors s3d's S3 object-versioning model where one table holds one row per
-- version (see docs/VERSIONING-SHARE-SEARCH-PROVENANCE-RESEARCH.md).
--
-- Two additions:
--   * `version_id`  — random opaque public handle for a version (callers reference
--                     vault_version_show --version <id>); never changes, not guessable.
--   * `seq`         — monotonic per-UUID ordering; the canonical "which is newest".
--                     IsCurrent remains the denormalized fastest "is this the live
--                     winner" flag (kept in sync on every write).
--
-- CRITICAL schema change: the previous unique index on `uuid` forced ONE row per UUID.
-- Versioning requires MANY rows sharing a UUID (one per version), so that unique index
-- must be replaced by a non-unique index (hot sync lookup by UUID) — or a uniqueness
-- on (uuid, version_id). We use (uuid, version_id) to keep the identity-lookup fast and
-- unambiguous while permitting many versions of one logical file.

-- 1. Add versioning columns.
ALTER TABLE `files` ADD COLUMN `version_id` text NOT NULL DEFAULT '';
ALTER TABLE `files` ADD COLUMN `seq` integer NOT NULL DEFAULT 0;

-- 2. Replace the unique-per-UUID index with one unique per (uuid, version_id) so a
--    logical file can have many version rows but each version is unique.
DROP INDEX IF EXISTS `idx_files_uuid`;
CREATE UNIQUE INDEX IF NOT EXISTS `idx_files_uuid_version` ON `files`(`uuid`, `version_id`);

-- +goose Down

-- Versioning is not reversible for a single version per UUID in general (rows were
-- split out). We drop the versioning index and restore a plain (non-unique) index on
-- uuid to keep the cache usable; we do NOT attempt to collapse multiple versions back
-- into one row. The version columns are left in place (dropping columns requires a
-- table rebuild; acceptable to keep them harmless on downgrade).
DROP INDEX IF EXISTS `idx_files_uuid_version`;
CREATE INDEX IF NOT EXISTS `idx_files_uuid` ON `files`(`uuid`);
