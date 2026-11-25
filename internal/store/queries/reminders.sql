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
    next_due_at,
    interval_count,
    month_week_position,
    month_weekday,
    start_date,
    end_kind,
    end_date,
    end_count
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
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
-- foreign id answer with no rows. The fired counter and the finished marker are
-- reset: a changed recurrence is a new cycle, exactly as the first one.
UPDATE reminders
SET message             = $3,
    timezone            = $4,
    schedule_kind       = $5,
    schedule_days       = $6,
    time_hhmm           = $7,
    repeat_minutes      = $8,
    enabled             = $9,
    next_due_at         = $10,
    interval_count      = $11,
    month_week_position = $12,
    month_weekday       = $13,
    start_date          = $14,
    end_kind            = $15,
    end_date            = $16,
    end_count           = $17,
    occurrences_fired   = 0,
    finished_at         = NULL,
    updated_at          = now()
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

-- name: CreateFiredOccurrence :one
-- The scheduler creates the row of a first firing. The id is generated before
-- the message is sent, because the «✅ Выполнено» button carries it in its
-- payload: the button must work even if this insert is delayed.
INSERT INTO reminder_occurrences (
    id,
    reminder_id,
    scheduled_at,
    first_sent_at,
    repeat_count,
    next_repeat_at,
    status,
    deliveries
) VALUES (
    $1, $2, $3, $4, 1, $5, 'active', $6
)
RETURNING *;

-- name: RecordOccurrenceRepeat :one
-- One repeat of an active occurrence: the counter grows, the next repeat moves
-- and the delivery refs point at the newest message (the previous one was
-- deleted). Only an active occurrence is touched, so a confirmation that won
-- the race is never overwritten.
UPDATE reminder_occurrences
SET repeat_count   = repeat_count + 1,
    next_repeat_at = $2,
    deliveries     = $3,
    updated_at     = $4
WHERE id = $1
  AND status = 'active'
RETURNING *;

-- name: GetOccurrence :one
-- One occurrence of one reminder. Callers classify the status themselves: a
-- confirmation answers differently for completed, cancelled and unknown rows.
SELECT * FROM reminder_occurrences
WHERE id = $1
  AND reminder_id = $2;

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

-- name: CompleteOccurrence :one
-- Confirms one specific occurrence of the owner's reminder. The WHERE clause is
-- the guard: only an active occurrence changes, and the owner check is part of
-- the same statement, so the chat button, the HTTP API and MCP have exactly one
-- winner and one answer.
UPDATE reminder_occurrences o
SET status         = 'completed',
    completed_at   = sqlc.arg(completed_at)::timestamptz,
    next_repeat_at = NULL,
    updated_at     = sqlc.arg(completed_at)::timestamptz
FROM reminders r
WHERE o.id = sqlc.arg(occurrence_id)::uuid
  AND o.reminder_id = r.id
  AND r.id = sqlc.arg(reminder_id)::uuid
  AND r.user_id = sqlc.arg(user_id)::uuid
  AND o.status = 'active'
RETURNING o.*;

-- name: GetReminderOwner :one
-- Deliberately not scoped by the owner: a confirmation has to tell "already
-- confirmed" from "not your reminder" from "gone", so the owner id is compared
-- in Go. The id comes from a button of that chat and is not enumerable.
SELECT user_id FROM reminders
WHERE id = $1;

-- name: ClaimDueReminders :many
-- Claims a batch of due work for one scheduler tick. The rows stay locked for
-- the rest of the short transaction, so two replicas never claim the same
-- reminder: SKIP LOCKED sends the second one to the next reminder instead of
-- making it wait. lock_until is the lease that protects the row while the
-- message travels through the messenger.
WITH due AS (
    SELECT r.id
    FROM reminders r
    WHERE r.enabled
      AND (r.lock_until IS NULL OR r.lock_until < sqlc.arg(now)::timestamptz)
      AND (
            (
                r.next_due_at IS NOT NULL
                AND r.next_due_at <= sqlc.arg(now)::timestamptz
                AND NOT EXISTS (
                    SELECT 1 FROM reminder_occurrences o
                    WHERE o.reminder_id = r.id
                      AND o.status = 'active'
                )
            )
            OR EXISTS (
                SELECT 1 FROM reminder_occurrences o
                WHERE o.reminder_id = r.id
                  AND o.status = 'active'
                  AND o.next_repeat_at IS NOT NULL
                  AND o.next_repeat_at <= sqlc.arg(now)::timestamptz
            )
      )
    ORDER BY r.next_due_at NULLS LAST, r.id
    LIMIT sqlc.arg(batch_size)::int
    FOR UPDATE SKIP LOCKED
)
UPDATE reminders r
SET lock_until = sqlc.arg(lock_until)::timestamptz
FROM due
WHERE r.id = due.id
RETURNING r.*;

-- name: FinishReminderFirstFire :one
-- Persists a successful first firing: the next due time moves from "now" (NULL
-- when the recurrence has no future occurrence), the fired counter grows, the
-- delivery error is gone and the lease is released. updated_at is part of the
-- guard, so a rule the user changed while the message was in flight is never
-- overwritten by the stale worker.
UPDATE reminders
SET next_due_at       = sqlc.arg(next_due_at),
    occurrences_fired = occurrences_fired + 1,
    last_error        = NULL,
    lock_until        = NULL,
    updated_at        = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(reminder_id)::uuid
  AND lock_until = sqlc.arg(lock_until)::timestamptz
  AND updated_at = sqlc.arg(unchanged_since)::timestamptz
RETURNING *;

-- name: FinishReminderRepeat :one
-- Releases the lease after a successful repeat. The schedule does not change:
-- next_due_at already points at the following firing of the rule.
UPDATE reminders
SET last_error = NULL,
    lock_until = NULL,
    updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(reminder_id)::uuid
  AND lock_until = sqlc.arg(lock_until)::timestamptz
RETURNING *;

-- name: FinishReminder :one
-- Closes a rule whose recurrence has no future occurrence: the last occurrence
-- was confirmed and the reminder must never fire again. It stays visible as
-- «завершено», so only `enabled` and `next_due_at` change, not the rule itself.
UPDATE reminders
SET enabled     = false,
    next_due_at = NULL,
    finished_at = sqlc.arg(finished_at)::timestamptz,
    updated_at  = sqlc.arg(finished_at)::timestamptz
WHERE id = sqlc.arg(reminder_id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
RETURNING *;

-- name: ReleaseReminderLease :execrows
-- Gives the lease back after a failed delivery without changing any state: the
-- next tick retries the same firing, so a messenger outage delays a reminder
-- instead of losing it.
UPDATE reminders
SET last_error = sqlc.arg(last_error),
    lock_until = NULL,
    updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(reminder_id)::uuid
  AND lock_until = sqlc.arg(lock_until)::timestamptz;
