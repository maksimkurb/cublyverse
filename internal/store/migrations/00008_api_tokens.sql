-- +goose Up
-- +goose StatementBegin
-- Personal access tokens (PAT) authenticate non-browser clients: MCP servers,
-- scripts and anything else that cannot carry a session cookie. Only the sha256
-- of the token is stored, so a database leak hands out no usable credential.
-- The visible prefix lets a user recognise the row in the WebApp.
CREATE TABLE api_tokens (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- First characters of the token after the "cv_pat_" marker. Not a secret:
    -- it identifies the row, the rest of the token never leaves the client.
    token_prefix text        NOT NULL,
    token_hash   bytea       NOT NULL,
    -- Scopes are validated in Go against a registry; the database only keeps
    -- the granted strings so a new scope needs no migration.
    scopes       text[]      NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_tokens_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT api_tokens_name_check CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT api_tokens_prefix_check CHECK (char_length(token_prefix) BETWEEN 1 AND 16),
    CONSTRAINT api_tokens_scopes_check CHECK (cardinality(scopes) > 0)
);

CREATE INDEX api_tokens_user_idx ON api_tokens (user_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE api_tokens;
-- +goose StatementEnd
