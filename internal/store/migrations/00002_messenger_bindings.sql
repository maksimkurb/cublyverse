-- +goose Up
-- +goose StatementBegin
CREATE TABLE messenger_bindings (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform        text        NOT NULL,
    remote_user_id  text        NOT NULL,
    conversation_id text        NOT NULL DEFAULT '',
    verified_at     timestamptz NOT NULL DEFAULT now(),
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messenger_bindings_remote_unique UNIQUE (platform, remote_user_id)
);

CREATE INDEX messenger_bindings_user_idx ON messenger_bindings (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE messenger_bindings;
-- +goose StatementEnd
