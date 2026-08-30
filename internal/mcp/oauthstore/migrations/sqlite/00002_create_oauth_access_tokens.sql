-- +goose Up

-- Short-lived access tokens are persisted so a connector that legitimately
-- holds an unexpired token (e.g. Grok's rmcp, which does not refresh on 401)
-- can resume after a server restart without being forced through a fresh
-- login just because the in-memory token map was wiped. They expire naturally
-- and are reaped with the refresh tokens.
CREATE TABLE oauth_access_tokens (
    token      TEXT PRIMARY KEY,
    client_id  TEXT NOT NULL DEFAULT '',
    resource   TEXT NOT NULL DEFAULT '',
    expires_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS oauth_access_tokens;
