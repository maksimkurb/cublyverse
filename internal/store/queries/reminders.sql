-- name: CreateReminder :one
INSERT INTO reminders (
    user_id,
    message,
    timezone,
    schedule_kind,
    schedule_days,
    time_hhmm,
    repeat_minutes,
    enabled,
    next_due_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetReminder :one
-- Ownership is part of the lookup: a foreign id is indistinguishable from an
-- unknown one.
SELECT * FROM reminders
WHERE id = $1
  AND user_id = $2;

-- name: ListReminders :many
SELECT * FROM reminders
WHERE user_id = $1
ORDER BY time_hhmm, created_at, id;

-- name: UpdateReminder :one
-- A full replacement of the rule (PUT semantics). The owner check makes a
-- foreign id answer with no rows.
UPDATE reminders
SET message        = $3,
    timezone       = $4,
    schedule_kind  = $5,
    schedule_days  = $6,
    time_hhmm      = $7,
    repeat_minutes = $8,
    enabled        = $9,
    next_due_at    = $10,
    updated_at     = now()
WHERE id = $1
  AND user_id = $2
RETURNING *;

-- name: DeleteReminder :execrows
DELETE FROM reminders
WHERE id = $1
  AND user_id = $2;

-- name: CreateOccurrence :one
-- The partial unique index allows at most one active occurrence per reminder,
-- so a second insert while one is active fails at the database level.
INSERT INTO reminder_occurrences (
    reminder_id,
    scheduled_at,
    status
) VALUES (
    $1, $2, 'active'
)
RETURNING *;

-- name: GetActiveOccurrence :one
SELECT * FROM reminder_occurrences
WHERE reminder_id = $1
  AND status = 'active';

-- name: ListActiveOccurrencesByUser :many
-- One query for the whole list answer: the active occurrence of every reminder
-- of the user.
SELECT o.* FROM reminder_occurrences o
JOIN reminders r ON r.id = o.reminder_id
WHERE r.user_id = $1
  AND o.status = 'active';

-- name: CancelActiveOccurrence :execrows
-- Editing or disabling a reminder drops its pending firing: the user asked for
-- something else, so the old message must not be repeated.
UPDATE reminder_occurrences
SET status         = 'cancelled',
    next_repeat_at = NULL,
    updated_at     = now()
WHERE reminder_id = $1
  AND status = 'active';

-- name: CompleteActiveOccurrence :one
-- The owner check lives in the same statement, so a foreign reminder can never
-- be completed even if the caller skips the lookup.
UPDATE reminder_occurrences o
SET status         = 'completed',
    completed_at   = $2,
    next_repeat_at = NULL,
    updated_at     = now()
FROM reminders r
WHERE o.reminder_id = r.id
  AND r.id = $1
  AND r.user_id = $3
  AND o.status = 'active'
RETURNING o.*;
