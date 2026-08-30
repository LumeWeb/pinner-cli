-- +goose Up

-- Add per-file staged-upload tracking.
--
-- The vault now accepts a write (Put) by buffering the plaintext to local disk
-- and recording a File row in status "pending" BEFORE any Sia interaction, so
-- an agent/MCP upload returns to the caller immediately instead of blocking 90+
-- seconds on a full host-set upload+pin. A background loop (or an explicit
-- `vault flush` / share forced-flush) drains pending rows: it packs several
-- small objects into shared slabs via the SDK's UploadPacked, pins each, and
-- transitions the row to "ok".
--
-- `staged_path` holds the on-disk plaintext buffer path while the object is not
-- yet durable ("pending"/"uploaded"). It is cleared (and the buffer file
-- deleted) once the object is pinned ("ok"). Local reads (Get/Cat) serve from
-- this path while pending; empty for any durable file.

ALTER TABLE `files` ADD COLUMN `staged_path` TEXT NOT NULL DEFAULT '';

-- Partial index over the (tiny) set of rows with a staged buffer. The flush
-- engine scans staged rows every cycle (`WHERE staged_path <> '' AND deleted_at
-- IS NULL`); only the not-yet-durable rows need indexing, and a partial index
-- keeps that set tiny even as the full files table grows with every version.
CREATE INDEX `idx_files_staged` ON `files`(`staged_path`) WHERE `staged_path` <> '';

-- +goose Down

DROP INDEX IF EXISTS `idx_files_staged`;
ALTER TABLE `files` DROP COLUMN `staged_path`;
