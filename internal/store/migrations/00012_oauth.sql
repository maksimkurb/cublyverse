-- +goose Up
-- +goose StatementBegin
-- OAuth 2.1 authorization server for MCP clients. Cublyverse issues the tokens
-- itself and authenticates the user through the existing SSO session, so the
-- identity provider does not need to support dynamic client registration.
--
-- Everything secret is stored as a sha256 hash, like personal access tokens.

-- Public clients only (token_endpoint_auth_method "none"): an MCP client is a
-- desktop or web app that cannot keep a secret, so PKCE is what protects the
-- code. client_id is either a random id (RFC 7591 dynamic registration) or the
-- HTTPS URL of a client metadata document (CIMD); "kind" tells which.
CREATE TABLE oauth_clients (
    client_id     text        PRIMARY KEY,
    kind          text        NOT NULL,
    name          text        NOT NULL,
    redirect_uris text[]      NOT NULL,
    -- For CIMD clients: when the document was last fetched, so it can be
    -- refreshed after a day.
    fetched_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oauth_clients_kind_check CHECK (kind IN ('dcr', 'cimd')),
    CONSTRAINT oauth_clients_redirects_check CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10)
);

-- A one-time authorization code. family_id ties it to the tokens it produced, so
-- a replayed code revokes them.
CREATE TABLE oauth_codes (
    code_hash      bytea       PRIMARY KEY,
    family_id      uuid        NOT NULL,
    client_id      text        NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id   uuid        REFERENCES workspaces (id) ON DELETE CASCADE,
    scopes         text[]      NOT NULL,
    resource       text        NOT NULL DEFAULT '',
    redirect_uri   text        NOT NULL,
    code_challenge text        NOT NULL,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- One row per issued access/refresh pair. A refresh grant marks the row used
-- and inserts the next row of the same family (rotation); presenting a used
-- refresh token revokes the whole family.
CREATE TABLE oauth_tokens (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id          uuid        NOT NULL,
    client_id          text        NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id       uuid        REFERENCES workspaces (id) ON DELETE CASCADE,
    scopes             text[]      NOT NULL,
    resource           text        NOT NULL DEFAULT '',
    access_hash        bytea       NOT NULL,
    access_expires_at  timestamptz NOT NULL,
    refresh_hash       bytea       NOT NULL,
    refresh_expires_at timestamptz NOT NULL,
    refresh_used_at    timestamptz,
    revoked_at         timestamptz,
    last_used_at       timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oauth_tokens_access_unique UNIQUE (access_hash),
    CONSTRAINT oauth_tokens_refresh_unique UNIQUE (refresh_hash)
);
CREATE INDEX oauth_tokens_user_idx ON oauth_tokens (user_id, created_at DESC);
CREATE INDEX oauth_tokens_family_idx ON oauth_tokens (family_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE oauth_tokens;
DROP TABLE oauth_codes;
DROP TABLE oauth_clients;
-- +goose StatementEnd
