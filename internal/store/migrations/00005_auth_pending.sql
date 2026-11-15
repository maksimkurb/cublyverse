-- +goose Up
-- +goose StatementBegin
-- Pending sign-in flows. A row is created when the bot sends a login button or
-- when a browser starts a plain SSO login, and it lives until the flow
-- completes or expires (10 minutes by default). Keeping this in PostgreSQL
-- means an SSO round trip survives a process restart.
--
-- state_hash is the sha256 of the state token that travels in the URL;
-- confirm_token_hash is the sha256 of the single-use confirmation token. The
-- PKCE verifier is a secret too, but it has to be replayed to the provider, so
-- it is stored as-is and deleted with the row.
CREATE TABLE auth_pending (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    state_hash            bytea       NOT NULL,
    status                text        NOT NULL,
    flow                  text        NOT NULL,
    nonce                 text        NOT NULL,
    verifier              text        NOT NULL,
    platform              text        NOT NULL DEFAULT '',
    remote_user_id        text        NOT NULL DEFAULT '',
    conversation_id       text        NOT NULL DEFAULT '',
    return_to             text        NOT NULL DEFAULT '',
    user_id               uuid        REFERENCES users (id) ON DELETE CASCADE,
    confirm_token_hash    bytea       NOT NULL DEFAULT '\x'::bytea,
    init_data_verified_at timestamptz,
    expires_at            timestamptz NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_pending_state_hash_unique UNIQUE (state_hash),
    CONSTRAINT auth_pending_status_check CHECK (status IN ('pending_oidc', 'awaiting_confirmation', 'completed')),
    CONSTRAINT auth_pending_flow_check CHECK (flow IN ('bind', 'browser'))
);

CREATE INDEX auth_pending_expires_idx ON auth_pending (expires_at);
CREATE INDEX auth_pending_binding_idx ON auth_pending (platform, remote_user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE auth_pending;
-- +goose StatementEnd
