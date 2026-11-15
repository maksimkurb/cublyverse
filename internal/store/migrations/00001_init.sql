-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    oidc_issuer  text        NOT NULL,
    oidc_subject text        NOT NULL,
    email        text,
    display_name text,
    timezone     text        NOT NULL DEFAULT 'UTC',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_oidc_identity_unique UNIQUE (oidc_issuer, oidc_subject)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd
