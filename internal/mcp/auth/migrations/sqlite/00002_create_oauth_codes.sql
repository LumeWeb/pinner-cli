-- +goose Up
CREATE TABLE oauth_authorization_codes (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at            DATETIME,
    updated_at            DATETIME,
    deleted_at            DATETIME,
    code                  TEXT NOT NULL,
    client_id             TEXT NOT NULL,
    redirect_uri          TEXT,
    code_challenge        TEXT,
    code_challenge_method TEXT,
    resource              TEXT,
    user_id               INTEGER NOT NULL,
    scope                 TEXT,
    expires_at            DATETIME NOT NULL,
    used_at               DATETIME
);
CREATE UNIQUE INDEX idx_oauth_codes_code ON oauth_authorization_codes (code);
CREATE INDEX idx_oauth_codes_client_id ON oauth_authorization_codes (client_id);
CREATE INDEX idx_oauth_codes_deleted_at ON oauth_authorization_codes (deleted_at);

-- +goose Down
DROP TABLE IF EXISTS oauth_authorization_codes;
