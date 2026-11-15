-- +goose Up
-- +goose StatementBegin
-- Messenger callbacks are addressed either by an opaque token (Telegram
-- callback_data) or by a message key (Matrix event + reaction key). Exactly one
-- of the two addressing modes is used per row, which is why both unique indexes
-- tolerate NULL columns: PostgreSQL treats NULLs as distinct, so token rows
-- never collide on the message key index and vice versa.
CREATE TABLE interactions (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- sha256 of the callback token; the token itself must never be stored.
    token_hash      bytea,
    platform        text,
    conversation_id text,
    message_id      text,
    action_key      text,
    action_id       text        NOT NULL DEFAULT '',
    choice_id       text        NOT NULL DEFAULT '',
    option_id       text        NOT NULL DEFAULT '',
    expected_user_id text       NOT NULL DEFAULT '',
    expires_at      timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT interactions_token_unique UNIQUE (token_hash),
    CONSTRAINT interactions_message_key_unique UNIQUE (platform, conversation_id, message_id, action_key)
);

CREATE INDEX interactions_expires_idx ON interactions (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE interactions;
-- +goose StatementEnd
