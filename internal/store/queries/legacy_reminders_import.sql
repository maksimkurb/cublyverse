-- name: InsertLegacyReminder :one
-- Stores one configuration row of the old n8n table. The unique source id makes
-- the import idempotent: a second run of the same export inserts nothing and
-- gets pgx.ErrNoRows, which the importer reports as a duplicate.
INSERT INTO legacy_reminders_import (
    source_reminder_id,
    telegram_user_id,
    message,
    timezone,
    schedule_kind,
    schedule_days,
    time_hhmm,
    repeat_minutes,
    enabled
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (source_reminder_id) DO NOTHING
RETURNING *;

-- name: ExistingLegacySourceIDs :many
-- Which rows of an export are already imported. A dry run uses it to report the
-- duplicates it would skip without writing anything.
SELECT source_reminder_id
FROM legacy_reminders_import
WHERE source_reminder_id = ANY(sqlc.arg(source_reminder_ids)::text[]);

-- name: CountPendingLegacyReminders :one
-- How many rows still wait for a Telegram binding. The import command prints it
-- as "waiting for a sign-in", so the answer is not "what did this run do" but
-- "how much of the old table is still inert".
SELECT count(*) FROM legacy_reminders_import
WHERE claimed_by IS NULL;

-- name: ClaimLegacyReminders :many
-- Atomically takes every pending row of one Telegram account and marks it as
-- attached to the user. FOR UPDATE SKIP LOCKED makes a second claimer of the
-- same account (two confirmations racing) take nothing instead of waiting, and
-- claimed_by IS NULL keeps an already claimed row out of reach of every later
-- claim, so a reminder is created at most once.
WITH pending AS (
    SELECT id
    FROM legacy_reminders_import
    WHERE telegram_user_id = sqlc.arg(telegram_user_id)::text
      AND claimed_by IS NULL
    ORDER BY created_at, id
    FOR UPDATE SKIP LOCKED
)
UPDATE legacy_reminders_import l
SET claimed_by = sqlc.arg(claimed_by)::uuid,
    claimed_at = now()
FROM pending
WHERE l.id = pending.id
RETURNING l.*;
