-- +goose Up
-- +goose StatementBegin
-- Every mini-app has its own spaces: sharing a warehouse does not share the
-- reminders. A user's «Личное» space of a mini-app is flagged personal; it is
-- where things go when nobody names a space.
ALTER TABLE workspaces
    ADD COLUMN app      text    NOT NULL DEFAULT 'warehouse',
    ADD COLUMN personal boolean NOT NULL DEFAULT false;
ALTER TABLE workspaces
    ALTER COLUMN app DROP DEFAULT,
    ADD CONSTRAINT workspaces_app_check CHECK (app IN ('warehouse', 'reminders'));
CREATE UNIQUE INDEX workspaces_personal_uidx ON workspaces (created_by, app) WHERE personal;

-- The oldest existing warehouse of every creator becomes their personal one.
UPDATE workspaces w
SET personal = true
WHERE w.id IN (
    SELECT DISTINCT ON (created_by) id
    FROM workspaces
    WHERE created_by IS NOT NULL
    ORDER BY created_by, created_at, id
);

-- Existing reminders move into a personal space of their author.
INSERT INTO workspaces (name, created_by, app, personal)
SELECT 'Личное', r.user_id, 'reminders', true
FROM reminders r
GROUP BY r.user_id;

INSERT INTO workspace_members (workspace_id, user_id, added_by)
SELECT w.id, w.created_by, w.created_by
FROM workspaces w
WHERE w.app = 'reminders';

ALTER TABLE reminders
    ADD COLUMN workspace_id uuid REFERENCES workspaces (id) ON DELETE CASCADE;

-- updated_at guards the scheduler against stale writes; a migration is not a
-- change of the rule, so it is left alone.
UPDATE reminders r
SET workspace_id = w.id
FROM workspaces w
WHERE w.created_by = r.user_id
  AND w.app = 'reminders'
  AND w.personal;

ALTER TABLE reminders
    ALTER COLUMN workspace_id SET NOT NULL;
CREATE INDEX reminders_workspace_idx ON reminders (workspace_id, time_hhmm);

-- Who gets the message. Only recipients that are still members of the space
-- receive it.
CREATE TABLE reminder_recipients (
    reminder_id uuid NOT NULL REFERENCES reminders (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (reminder_id, user_id)
);

INSERT INTO reminder_recipients (reminder_id, user_id)
SELECT id, user_id FROM reminders;

-- There is one MCP resource now (/mcp), so a grant is not bound to a space.
-- Grants of the removed per-space endpoints can no longer be used.
DELETE FROM oauth_codes WHERE resource LIKE '%/mcp/w/%';
DELETE FROM oauth_tokens WHERE resource LIKE '%/mcp/w/%';
ALTER TABLE oauth_codes DROP COLUMN workspace_id;
ALTER TABLE oauth_tokens DROP COLUMN workspace_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The old schema has no spaces for reminders; their spaces are removed after
-- the link is dropped, so the reminders themselves stay.
ALTER TABLE oauth_codes ADD COLUMN workspace_id uuid REFERENCES workspaces (id) ON DELETE CASCADE;
ALTER TABLE oauth_tokens ADD COLUMN workspace_id uuid REFERENCES workspaces (id) ON DELETE CASCADE;
DROP TABLE reminder_recipients;
ALTER TABLE reminders DROP COLUMN workspace_id;
DELETE FROM workspaces WHERE app = 'reminders';
DROP INDEX workspaces_personal_uidx;
ALTER TABLE workspaces
    DROP COLUMN app,
    DROP COLUMN personal;
-- +goose StatementEnd
