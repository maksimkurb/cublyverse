-- +goose Up
-- +goose StatementBegin
-- Flexible recurrence, as a calendar app describes it.
--
-- `schedule_kind` grows from weekly|monthly to once|daily|weekly|monthly|yearly,
-- and a rule gains the pieces the new kinds need:
--
--   * interval_count     — every N days/weeks/months/years;
--   * start_date         — the first possible day (civil date in `timezone`);
--   * month_week_position + month_weekday — the nth weekday of a month
--                          (1..4, -1 = last), for "every second Tuesday";
--   * end_kind/end_date/end_count — never, until a date, or after N occurrences;
--   * occurrences_fired  — how many occurrences already fired, for a count end;
--   * finished_at        — set when the recurrence has no future occurrence and
--                          the last one was confirmed.
--
-- The columns are additive and every existing row is backfilled to the old
-- behaviour: interval 1, no end, no nth weekday, start date = the creation day
-- in the reminder's own time zone. Interval 1 has no alignment relative to the
-- start date, so every stored `next_due_at` stays exactly what the old rule
-- computed.
ALTER TABLE reminders
    ADD COLUMN interval_count      integer     NOT NULL DEFAULT 1,
    ADD COLUMN month_week_position smallint,
    ADD COLUMN month_weekday       smallint,
    ADD COLUMN start_date          date,
    ADD COLUMN end_kind            text        NOT NULL DEFAULT 'none',
    ADD COLUMN end_date            date,
    ADD COLUMN end_count           integer,
    ADD COLUMN occurrences_fired   integer     NOT NULL DEFAULT 0,
    ADD COLUMN finished_at         timestamptz;

-- The first possible day of an existing rule is the day it was created. A past
-- date never restricts a schedule, so old rows keep their current next_due_at.
UPDATE reminders
SET start_date = (created_at AT TIME ZONE timezone)::date
WHERE start_date IS NULL;

ALTER TABLE reminders
    ALTER COLUMN start_date SET NOT NULL;

-- The old constraints only know two kinds and required at least one day. The new
-- kinds carry their days in other fields (a nth weekday in month_weekday, a
-- once/yearly rule in start_date), so the shape check becomes kind-specific.
ALTER TABLE reminders
    DROP CONSTRAINT reminders_schedule_kind,
    DROP CONSTRAINT reminders_days_not_empty,
    DROP CONSTRAINT reminders_days_range;

ALTER TABLE reminders
    ADD CONSTRAINT reminders_schedule_kind CHECK (
        schedule_kind IN ('once', 'daily', 'weekly', 'monthly', 'yearly')
    ),
    ADD CONSTRAINT reminders_interval_count CHECK (interval_count BETWEEN 1 AND 1000),
    ADD CONSTRAINT reminders_month_week_position CHECK (
        month_week_position IS NULL OR month_week_position IN (1, 2, 3, 4, -1)
    ),
    ADD CONSTRAINT reminders_month_weekday CHECK (
        month_weekday IS NULL OR month_weekday BETWEEN 1 AND 7
    ),
    ADD CONSTRAINT reminders_month_week_pair CHECK (
        (month_week_position IS NULL AND month_weekday IS NULL)
        OR (month_week_position IS NOT NULL AND month_weekday IS NOT NULL)
    ),
    ADD CONSTRAINT reminders_days_shape CHECK (
        CASE
            WHEN schedule_kind = 'weekly' THEN
                cardinality(schedule_days) > 0
                AND schedule_days <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]
            WHEN schedule_kind = 'monthly' AND month_week_position IS NULL THEN
                cardinality(schedule_days) > 0
                AND schedule_days <@ ARRAY[
                    1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
                    11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
                    21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31
                ]::smallint[]
            ELSE
                cardinality(schedule_days) = 0
        END
    ),
    ADD CONSTRAINT reminders_monthly_shape CHECK (
        schedule_kind <> 'monthly'
        OR (month_week_position IS NULL) = (cardinality(schedule_days) > 0)
    ),
    ADD CONSTRAINT reminders_end_kind CHECK (end_kind IN ('none', 'until', 'count')),
    ADD CONSTRAINT reminders_end_shape CHECK (
        (end_kind = 'none' AND end_date IS NULL AND end_count IS NULL)
        OR (end_kind = 'until' AND end_date IS NOT NULL AND end_count IS NULL)
        OR (end_kind = 'count' AND end_date IS NULL AND end_count IS NOT NULL AND end_count > 0)
    ),
    ADD CONSTRAINT reminders_occurrences_fired CHECK (occurrences_fired >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The old schema cannot represent the new recurrence kinds or an nth-weekday
-- rule, so removing them is destructive. This is a local-experiment rollback;
-- in production a schema rollback is a database restore.
DELETE FROM reminders
WHERE schedule_kind NOT IN ('weekly', 'monthly')
   OR month_week_position IS NOT NULL;

ALTER TABLE reminders
    DROP CONSTRAINT reminders_schedule_kind,
    DROP CONSTRAINT reminders_interval_count,
    DROP CONSTRAINT reminders_month_week_position,
    DROP CONSTRAINT reminders_month_weekday,
    DROP CONSTRAINT reminders_month_week_pair,
    DROP CONSTRAINT reminders_days_shape,
    DROP CONSTRAINT reminders_monthly_shape,
    DROP CONSTRAINT reminders_end_kind,
    DROP CONSTRAINT reminders_end_shape,
    DROP CONSTRAINT reminders_occurrences_fired;

ALTER TABLE reminders
    ADD CONSTRAINT reminders_schedule_kind CHECK (schedule_kind IN ('weekly', 'monthly')),
    ADD CONSTRAINT reminders_days_not_empty CHECK (cardinality(schedule_days) > 0),
    ADD CONSTRAINT reminders_days_range CHECK (
        CASE schedule_kind
            WHEN 'weekly' THEN schedule_days <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]
            WHEN 'monthly' THEN schedule_days <@ ARRAY[
                1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
                11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
                21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31
            ]::smallint[]
        END
    );

ALTER TABLE reminders
    DROP COLUMN interval_count,
    DROP COLUMN month_week_position,
    DROP COLUMN month_weekday,
    DROP COLUMN start_date,
    DROP COLUMN end_kind,
    DROP COLUMN end_date,
    DROP COLUMN end_count,
    DROP COLUMN occurrences_fired,
    DROP COLUMN finished_at;
-- +goose StatementEnd
