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

-- +goose Down

ALTER TABLE `files` DROP COLUMN `staged_path`;
