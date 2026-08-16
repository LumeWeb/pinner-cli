-- +goose Up

-- Vault per-file lifecycle status + provenance (P1 foundation).
-- Mirrors the Sia storage app's per-file state column + lostReason for
-- terminally-unrecoverable content, and adds first-class provenance audit
-- fields (created_by / agent_id / session_id) carried in the Sia object's
-- sealed metadata and synced to every device.
--
--   * `status`      — "ok" (default) | "pending" | "lost". Files flagged lost
--                     stay listed (never tombstoned) so an agent can enumerate
--                     and recover them.
--   * `lost_reason` — terminal detail (e.g. slab-unavailable error) when a file
--                     is flagged "lost".
--   * `created_by` / `agent_id` / `session_id` — best-effort, user-attested
--                     audit fields (no signing authority on a permissionless
--                     network), written once at upload and syncable.

ALTER TABLE `files` ADD COLUMN `status` text NOT NULL DEFAULT 'ok';
ALTER TABLE `files` ADD COLUMN `lost_reason` text NOT NULL DEFAULT '';
ALTER TABLE `files` ADD COLUMN `created_by` text NOT NULL DEFAULT '';
ALTER TABLE `files` ADD COLUMN `agent_id` text NOT NULL DEFAULT '';
ALTER TABLE `files` ADD COLUMN `session_id` text NOT NULL DEFAULT '';

-- Fast aggregate: how many live current files are "lost" (vault_status LostCount)
-- and the search backstop for `vault_search --status=lost`.
CREATE INDEX IF NOT EXISTS `idx_files_status` ON `files`(`status`) WHERE `is_current` = 1 AND `deleted_at` IS NULL;

-- +goose Down

-- Dropping columns requires a table rebuild in SQLite; leave the columns in
-- place (harmless on downgrade) and just drop the status index we added.
DROP INDEX IF EXISTS `idx_files_status`;
