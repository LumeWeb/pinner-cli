-- +goose Up

-- Make a stuck "pending" file visible.
--
-- A staged (pending) file is flushed by the background upload loop, a
-- `vault flush`, or an MCP `vault_flush`. If the host pin is slow or the flush
-- keeps failing, the row previously sat in "pending" forever with no way to
-- tell "still uploading" from "flush is failing". These columns surface that:
-- flush_attempts counts how many flush passes have tried to durable-upload the
-- row, and flush_error holds the most recent failure detail (empty until the
-- first failure). Both are cleared the moment the flush succeeds (status -> ok).

ALTER TABLE `files` ADD COLUMN `flush_attempts` INTEGER NOT NULL DEFAULT 0;
ALTER TABLE `files` ADD COLUMN `flush_error` TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE `files` DROP COLUMN `flush_error`;
ALTER TABLE `files` DROP COLUMN `flush_attempts`;
