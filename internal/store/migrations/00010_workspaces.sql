-- +goose Up
-- +goose StatementBegin
-- A workspace is a shared space: everyone who is a member sees and edits the
-- same data. There are no roles; membership is full access. One user can belong
-- to several workspaces. Members are added by picking an existing user (someone
-- who has signed in at least once), so there are no invite links or tokens.
CREATE TABLE workspaces (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    created_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workspaces_name_check CHECK (char_length(name) BETWEEN 1 AND 80)
);

CREATE TABLE workspace_members (
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

-- "Which workspaces does this user belong to" is the hot lookup of every
-- authorisation check.
CREATE INDEX workspace_members_user_idx ON workspace_members (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE workspace_members;
DROP TABLE workspaces;
-- +goose StatementEnd
