-- +goose Up

-- Distinguish a flush that is actively progressing from one that is hung on
-- the first attempt.
--
-- flush_attempts only increments once per worker start, so a long host-set
-- upload and a hung first attempt both show "flushing, attempts: 1" forever.
-- flush_started_at records when the CURRENT flush attempt began (RFC3339). A
-- swarm polling vault_stat can compute elapsed = now - flush_started_at and
-- fail a pin that has been flushing without terminating longer than its own
-- hang threshold. It is cleared when the flush succeeds (status -> ok) and
-- refreshed on each retry pass, so a genuinely progressing (retrying) file
-- shows a fresh started_at rather than an ever-staler one.

ALTER TABLE `files` ADD COLUMN `flush_started_at` TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE `files` DROP COLUMN `flush_started_at`;
