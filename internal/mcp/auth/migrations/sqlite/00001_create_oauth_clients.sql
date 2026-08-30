-- +goose Up
CREATE TABLE oauth_clients (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at                 DATETIME,
    updated_at                 DATETIME,
    deleted_at                 DATETIME,
    client_id                  TEXT NOT NULL,
    client_name                TEXT,
    redirect_uris              TEXT,
    grant_types                TEXT,
    response_types             TEXT,
    token_endpoint_auth_method TEXT,
    scopes                     TEXT,
    user_id                    INTEGER,
    is_active                  INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX idx_oauth_clients_client_id ON oauth_clients (client_id);
CREATE INDEX idx_oauth_clients_user_id ON oauth_clients (user_id);
CREATE INDEX idx_oauth_clients_deleted_at ON oauth_clients (deleted_at);

-- +goose Down
DROP TABLE IF EXISTS oauth_clients;
