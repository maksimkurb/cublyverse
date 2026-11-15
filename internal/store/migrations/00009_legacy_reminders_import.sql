-- +goose Up
-- +goose StatementBegin
-- Waiting room for reminders of the old n8n bot «Cubly Reminder».
--
-- The legacy system stored a reminder per Telegram user id, while Cublyverse
-- stores it per SSO user. The import therefore keeps the configuration row
-- (the rule) together with the Telegram account that owned it, and turns it into
-- a real reminder of `reminders` when that Telegram account is bound to an SSO
-- user. Rows stay here as the record of what was imported: `claimed_by` marks
-- the ones that already became a reminder.
--
-- Only configuration is imported. Nothing of the in-flight state of the old
-- workflow (awaiting_confirmation, message ids, locks, counters) is copied: a
-- claimed reminder starts fresh with next_due_at computed from the current time.
CREATE TABLE legacy_reminders_import (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Id of the row in the n8n Data Table. Unique, so re-running the import of
    -- the same export never duplicates a reminder.
    source_reminder_id text        NOT NULL,
    -- Telegram user id of the old bot; a binding of this account claims the row.
    telegram_user_id   text        NOT NULL,
    message            text        NOT NULL,
    timezone           text        NOT NULL,
    schedule_kind      text        NOT NULL,
    schedule_days      smallint[]  NOT NULL,
    time_hhmm          text        NOT NULL,
    repeat_minutes     integer     NOT NULL,
    enabled            boolean     NOT NULL DEFAULT true,
    -- SSO user the row was attached to, and when. NULL means still waiting for
    -- a Telegram binding. ON DELETE SET NULL keeps the history and makes the row
    -- claimable again if the user is deleted.
    claimed_by         uuid        REFERENCES users (id) ON DELETE SET NULL,
    claimed_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT legacy_reminders_import_source_unique UNIQUE (source_reminder_id),
    CONSTRAINT legacy_reminders_import_kind CHECK (schedule_kind IN ('weekly', 'monthly')),
    CONSTRAINT legacy_reminders_import_days_not_empty CHECK (cardinality(schedule_days) > 0),
    CONSTRAINT legacy_reminders_import_days_range CHECK (
        CASE schedule_kind
            WHEN 'weekly' THEN schedule_days <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]
            WHEN 'monthly' THEN schedule_days <@ ARRAY[
                1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
                11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
                21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31
            ]::smallint[]
        END
    ),
    CONSTRAINT legacy_reminders_import_time_hhmm CHECK (time_hhmm ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    CONSTRAINT legacy_reminders_import_repeat_minutes CHECK (repeat_minutes BETWEEN 1 AND 1440)
);

-- Claiming looks up the pending rows of one Telegram account.
CREATE INDEX legacy_reminders_import_telegram_idx ON legacy_reminders_import (telegram_user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE legacy_reminders_import;
-- +goose StatementEnd
