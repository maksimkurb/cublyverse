-- +goose Up
-- +goose StatementBegin
-- Session tokens are opaque 32-byte values handed to the client. Only their
-- sha256 is stored, so a database leak does not hand out live sessions.
CREATE TABLE sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   bytea       NOT NULL,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- bearer: Telegram Mini App; cookie: browser session.
    kind         text        NOT NULL,
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sessions_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT sessions_kind_check CHECK (kind IN ('bearer', 'cookie'))
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sessions;
-- +goose StatementEnd
