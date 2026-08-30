-- +goose Up
CREATE TABLE oauth_refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    token      TEXT NOT NULL,
    client_id  TEXT NOT NULL,
    resource   TEXT,
    user_id    INTEGER NOT NULL,
    chain_root TEXT,
    expires_at DATETIME NOT NULL,
    used_at    DATETIME,
    revoked    INTEGER NOT NULL DEFAULT 0,
    successor  TEXT
);
CREATE UNIQUE INDEX idx_oauth_refresh_token ON oauth_refresh_tokens (token);
CREATE INDEX idx_oauth_refresh_client_id ON oauth_refresh_tokens (client_id);
CREATE INDEX idx_oauth_refresh_chain_root ON oauth_refresh_tokens (chain_root);
CREATE INDEX idx_oauth_refresh_deleted_at ON oauth_refresh_tokens (deleted_at);

CREATE TABLE oauth_access_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    token      TEXT NOT NULL,
    client_id  TEXT NOT NULL,
    resource   TEXT,
    user_id    INTEGER NOT NULL,
    expires_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_oauth_access_token ON oauth_access_tokens (token);
CREATE INDEX idx_oauth_access_client_id ON oauth_access_tokens (client_id);
CREATE INDEX idx_oauth_access_deleted_at ON oauth_access_tokens (deleted_at);

-- +goose Down
DROP TABLE IF EXISTS oauth_access_tokens;
DROP TABLE IF EXISTS oauth_refresh_tokens;
