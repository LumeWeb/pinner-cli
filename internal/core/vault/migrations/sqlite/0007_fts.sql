-- +goose Up

-- Vault file-name full-text search index.
--
-- Uses the SQLite FTS5 trigram tokenizer over the `files.name` column only.
-- The trigram tokenizer indexes every contiguous 3-character sequence, so it
-- supports arbitrary SUBSTRING matching (not just whole-token matching), which
-- is exactly the semantics of the existing `LIKE '%<name>%'` name filter while
-- being index-backed instead of a full table scan. FTS5 also provides the
-- bm25() ranking function so result order can prefer relevance.
--
-- This is a REGULAR FTS5 table (not external-content): it stores its own copy
-- of `name`, keyed by `files.id` in rowid. External-content mode
-- (content='files', content_rowid='id') was deliberately NOT used: with the
-- trigram tokenizer, the external-content ''delete''/incremental sync written
-- by AFTER UPDATE triggers inside a Put transaction corrupts the index
-- ("database disk image is malformed"). A regular table with explicit
-- DELETE/INSERT triggers is corruption-free and passes PRAGMA integrity_check.
-- The duplicated `name` text is negligible for a vault of file names.
--
-- The index is kept in sync by triggers below. Only LIVE current rows are
-- indexed: soft-deleted (deleted_at NOT NULL) and historical (is_current = 0)
-- rows must never be searchable, mirroring the Search() WHERE clause
-- (`files.is_current = 1 AND files.deleted_at IS NULL`).
--
-- FTS5 requires the driver to be compiled with the `sqlite_fts5` build tag
-- (see the Makefile TAGS variable), which `make build`/`install`/`test` always
-- pass. Creating this table without FTS5 fails at the CREATE VIRTUAL TABLE
-- and aborts migration, so the build tag is mandatory. In addition, Search()
-- feature-detects at runtime (sqlite_compileoption_used('ENABLE_FTS5') and a
-- table-existence check) and falls back to the original LIKE path whenever
-- this table is unusable, keeping the pre-change behavior intact.

CREATE VIRTUAL TABLE IF NOT EXISTS `files_fts` USING fts5(
    `name`,
    tokenize='trigram'
);

-- Backfill from existing live rows (kept idempotent for safety).
INSERT INTO `files_fts`(`rowid`, `name`)
SELECT `id`, `name`
FROM `files`
WHERE `is_current` = 1 AND `deleted_at` IS NULL;

-- Keep files_fts in sync with the files table for live current rows only.
-- Each trigger is multi-statement (BEGIN...END), so goose requires the
-- StatementBegin/StatementEnd annotations or its line splitter would truncate
-- the trigger at the first inner semicolon ("incomplete input").
-- INSERT: index only live current rows (FTS5 inserts are idempotent per rowid).
-- +goose StatementBegin
CREATE TRIGGER `files_fts_ai` AFTER INSERT ON `files` BEGIN
    INSERT INTO `files_fts`(`rowid`, `name`)
    SELECT NEW.`id`, NEW.`name`
    WHERE NEW.`is_current` = 1 AND NEW.`deleted_at` IS NULL;
END;
-- +goose StatementEnd

-- UPDATE: drop the old row, then re-insert iff the row is still live+current.
-- This covers name changes on a live row, is_current demotion/promotion, and
-- soft-delete (deleted_at getting set) in one trigger.
-- +goose StatementBegin
CREATE TRIGGER `files_fts_au` AFTER UPDATE ON `files` BEGIN
    DELETE FROM `files_fts` WHERE `rowid` = OLD.`id`;
    INSERT INTO `files_fts`(`rowid`, `name`)
    SELECT NEW.`id`, NEW.`name`
    WHERE NEW.`is_current` = 1 AND NEW.`deleted_at` IS NULL;
END;
-- +goose StatementEnd

-- DELETE: remove the index row for a hard-deleted file.
-- +goose StatementBegin
CREATE TRIGGER `files_fts_ad` AFTER DELETE ON `files` BEGIN
    DELETE FROM `files_fts` WHERE `rowid` = OLD.`id`;
END;
-- +goose StatementEnd

-- +goose Down

DROP TRIGGER IF EXISTS `files_fts_ai`;
DROP TRIGGER IF EXISTS `files_fts_au`;
DROP TRIGGER IF EXISTS `files_fts_ad`;
DROP TABLE IF EXISTS `files_fts`;
