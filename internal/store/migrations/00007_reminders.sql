-- +goose Up
-- +goose StatementBegin
-- A reminder is a rule: what to say, when to say it and how often to repeat it
-- until the user confirms. The owner is always the SSO user (users.id), never a
-- messenger account, so a reminder survives re-binding the messenger.
CREATE TABLE reminders (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    message        text        NOT NULL,
    -- IANA time zone the schedule is interpreted in.
    timezone       text        NOT NULL,
    -- 'weekly' (ISO weekdays, 1 = Monday) or 'monthly' (days of month).
    schedule_kind  text        NOT NULL,
    -- 1..7 for weekly, 1..31 for monthly. A monthly day that the month does not
    -- have means the last day of that month (31 -> 28/29 February).
    schedule_days  smallint[]  NOT NULL,
    -- Local wall clock time as HH:MM (24h, zero padded).
    time_hhmm      text        NOT NULL,
    repeat_minutes integer     NOT NULL DEFAULT 10,
    enabled        boolean     NOT NULL DEFAULT true,
    -- Next occurrence in UTC. NULL while the reminder is disabled.
    next_due_at    timestamptz,
    -- Last delivery error; cleared after the next successful delivery.
    last_error     text,
    -- Lease of the scheduler: a tick holds the row while it sends.
    lock_until     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reminders_message_len CHECK (char_length(message) BETWEEN 1 AND 1500),
    CONSTRAINT reminders_time_hhmm CHECK (time_hhmm ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    CONSTRAINT reminders_repeat_minutes CHECK (repeat_minutes BETWEEN 1 AND 1440),
    CONSTRAINT reminders_schedule_kind CHECK (schedule_kind IN ('weekly', 'monthly')),
    CONSTRAINT reminders_days_not_empty CHECK (cardinality(schedule_days) > 0),
    CONSTRAINT reminders_days_range CHECK (
        CASE schedule_kind
            WHEN 'weekly' THEN schedule_days <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]
            WHEN 'monthly' THEN schedule_days <@ ARRAY[
                1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
                11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
                21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31
            ]::smallint[]
        END
    )
);

-- The scheduler only scans enabled reminders whose due time has come.
CREATE INDEX reminders_due_idx ON reminders (next_due_at)
    WHERE enabled AND next_due_at IS NOT NULL;
-- Listing is always scoped by the owner.
CREATE INDEX reminders_user_idx ON reminders (user_id, created_at);

-- An occurrence is one firing of a reminder: the message was sent, the user may
-- confirm it, and until then it repeats every repeat_minutes.
CREATE TABLE reminder_occurrences (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    reminder_id    uuid        NOT NULL REFERENCES reminders (id) ON DELETE CASCADE,
    -- When the occurrence was due, in UTC.
    scheduled_at   timestamptz NOT NULL,
    first_sent_at  timestamptz,
    repeat_count   integer     NOT NULL DEFAULT 0,
    next_repeat_at timestamptz,
    status         text        NOT NULL DEFAULT 'active',
    completed_at   timestamptz,
    -- Per-transport delivery results (which messenger got which message id).
    deliveries     jsonb       NOT NULL DEFAULT '[]'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reminder_occurrences_status CHECK (status IN ('active', 'completed', 'cancelled')),
    CONSTRAINT reminder_occurrences_repeat_count CHECK (repeat_count >= 0)
);

-- At most one active occurrence per reminder. A partial unique index enforces
-- the rule for every writer, not only for the Go service.
CREATE UNIQUE INDEX reminder_occurrences_active_uidx ON reminder_occurrences (reminder_id)
    WHERE status = 'active';
-- Repeats are found by the same scheduler tick as fresh occurrences.
CREATE INDEX reminder_occurrences_repeat_idx ON reminder_occurrences (next_repeat_at)
    WHERE status = 'active';
-- History of one reminder, newest first.
CREATE INDEX reminder_occurrences_reminder_idx ON reminder_occurrences (reminder_id, scheduled_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE reminder_occurrences;
DROP TABLE reminders;
-- +goose StatementEnd
