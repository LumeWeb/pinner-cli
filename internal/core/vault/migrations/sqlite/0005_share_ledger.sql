-- +goose Up

-- Agent-native share audit ledger (P1, A2A copy-once).
--
-- The Sia SDK's share primitive is a time-limited, read-only, single-object
-- bearer URL (CreateSharedObjectURL). Local SQLite can NEVER gate a
-- permissionless Sia blob, so an ACL/grants table would be theater. The only
-- meaningful A2A model is COPY-ONCE pin-to-indexer: the issuer hands out an
-- expiring share URL, the acceptor resolves it and pins a self-contained copy
-- into their OWN profile.
--
-- The one legitimate SQLite role is an append-only LEDGER (audit history), NOT
-- enforcement. Every vault_share_accept appends one row recording which share
-- was accepted, into which profile, and when. This table is write-only append;
-- it is never consulted to permit or deny access.

CREATE TABLE IF NOT EXISTS `share_ledger`
(
    `id`                integer PRIMARY KEY AUTOINCREMENT,
    `shared_vault_path` text    NOT NULL,
    `object_key`        text    NOT NULL,
    `expiry`            datetime,
    `target_principal`  text    NOT NULL DEFAULT '',
    `created_by`        text    NOT NULL DEFAULT '',
    `created_at`        datetime NOT NULL
);

-- Allow chronological review of accept activity.
CREATE INDEX IF NOT EXISTS `idx_share_ledger_created_at` ON `share_ledger`(`created_at` DESC);

-- +goose Down

DROP INDEX IF EXISTS `idx_share_ledger_created_at`;
DROP TABLE IF EXISTS `share_ledger`;
