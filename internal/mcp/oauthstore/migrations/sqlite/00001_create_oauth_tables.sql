-- +goose Up
CREATE TABLE oauth_clients (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    redirect_uris TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL
);

CREATE TABLE oauth_refresh_tokens (
    token      TEXT PRIMARY KEY,
    client_id  TEXT NOT NULL,
    resource   TEXT NOT NULL DEFAULT '',
    chain_root TEXT NOT NULL,
    issued_at  DATETIME NOT NULL,
    used_at    DATETIME,
    expires_at DATETIME NOT NULL,
    revoked    INTEGER NOT NULL DEFAULT 0,
    successor  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_oauth_refresh_chain_root ON oauth_refresh_tokens (chain_root);

-- +goose Down
DROP TABLE IF EXISTS oauth_refresh_tokens;
DROP TABLE IF EXISTS oauth_clients;
