package reminders

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Scheduler defaults.
const (
	// DefaultTickInterval is how often the scheduler looks for due work. A
	// fifteen second tick keeps a reminder within a quarter of a minute of its
	// scheduled time without loading the database.
	DefaultTickInterval = 15 * time.Second
	// DefaultLeaseDuration is how long a claimed reminder stays reserved for one
	// worker. It comfortably covers one messenger round trip and is much shorter
	// than the shortest repeat interval (one minute), so a crashed worker never
	// delays a reminder for long.
	DefaultLeaseDuration = 55 * time.Second
	// DefaultBatchSize bounds one tick. A batch is processed one reminder at a
	// time, so a big backlog trickles out instead of opening hundreds of
	// deliveries at once.
	DefaultBatchSize = 10
	// maxErrorMessage bounds what is stored in reminders.last_error: enough to
	// diagnose a messenger failure, short enough to fit a list answer.
	maxErrorMessage = 1000
)

// ErrLeaseLost means the lease of a reminder was taken over (or the rule was
// changed) while the message was in flight. The result of that delivery is
// dropped: a newer worker or a newer rule already owns the state.
var ErrLeaseLost = errors.New("reminders: lease lost")

// SchedulerConfig tunes the scheduler.
type SchedulerConfig struct {
	// Interval is how often the job looks for due work. Zero means
	// DefaultTickInterval.
	Interval time.Duration
	// Lease is how long a claim is reserved for one worker. Zero means
	// DefaultLeaseDuration.
	Lease time.Duration
	// BatchSize is how many reminders one tick processes. Zero means
	// DefaultBatchSize.
	BatchSize int
	// Logger records delivery failures and lease takeovers.
	Logger *slog.Logger
}

// Scheduler fires reminders: it finds due work, sends the message through the
// delivery and only then persists the state.
//
// The split matters. A short transaction claims the rows (FOR UPDATE SKIP LOCKED
// plus a lease in lock_until), the messenger call happens outside any
// transaction, and a second transaction persists the result under the same
// lease. A slow messenger therefore never holds a database lock, and a stale
// worker can never overwrite the state of a newer one.
type Scheduler struct {
	service    *Service
	interval   time.Duration
	lease      time.Duration
	batch      int
	logger     *slog.Logger
	trigger    chan struct{}
	noDelivery sync.Once
}

var _ interface {
	Name() string
	Run(ctx context.Context) error
} = (*Scheduler)(nil)

// NewScheduler returns a scheduler over service. The service owns the clock and
// the delivery, so a test injects both once.
func NewScheduler(service *Service, cfg SchedulerConfig) *Scheduler {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultTickInterval
	}
	if cfg.Lease <= 0 {
		cfg.Lease = DefaultLeaseDuration
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.Logger == nil {
		cfg.Logger = service.logger
	}
	return &Scheduler{
		service:  service,
		interval: cfg.Interval,
		lease:    cfg.Lease,
		batch:    cfg.BatchSize,
		logger:   cfg.Logger,
		trigger:  make(chan struct{}, 1),
	}
}

// Name identifies the job in logs.
func (s *Scheduler) Name() string { return "reminders.scheduler" }

// Run ticks until ctx is cancelled. A failed tick is logged and does not stop
// the job: a messenger outage delays reminders instead of killing the scheduler.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-s.trigger:
		}

		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "reminders tick failed", slog.String("error", err.Error()))
		}
	}
}

// Trigger asks a running scheduler for an immediate tick. It never blocks: a
// second trigger while one is pending is redundant. Tests use it instead of
// waiting for the interval.
func (s *Scheduler) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Tick processes one batch of due reminders and reports what went wrong on the
// way. A reminder whose delivery failed is recorded in last_error and retried on
// the next tick; the returned error only describes infrastructure problems and
// the results that had to be dropped.
func (s *Scheduler) Tick(ctx context.Context) error {
	if s.service.delivery == nil {
		s.noDelivery.Do(func() {
			s.logger.WarnContext(ctx, "reminders scheduler has no messenger delivery; nothing will fire")
		})
		return nil
	}

	now := s.service.now()
	lease := now.Add(s.lease)
	claims, err := s.claimDue(ctx, now, lease)
	if err != nil {
		return err
	}

	var errs error
	for _, claim := range claims {
		if err := s.deliver(ctx, claim, now, lease); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// claim is one reminder reserved by this tick together with the active
// occurrence, if the reminder is repeating rather than firing.
type claim struct {
	reminder   db.Reminder
	occurrence *db.ReminderOccurrence
}

// claimDue reserves due reminders for this worker. Everything happens in one
// short transaction: the rows are locked, the lease is stamped and the active
// occurrences are read, so the messenger calls that follow see a consistent
// picture and hold no lock.
func (s *Scheduler) claimDue(ctx context.Context, now, lease time.Time) ([]claim, error) {
	var claims []claim
	err := s.service.st.InTx(ctx, func(q *db.Queries) error {
		rows, err := q.ClaimDueReminders(ctx, db.ClaimDueRemindersParams{
			Now:       now,
			LockUntil: lease,
			BatchSize: int32(s.batch),
		})
		if err != nil {
			return fmt.Errorf("claim due reminders: %w", err)
		}
		for _, row := range rows {
			occurrence, err := q.GetActiveOccurrence(ctx, row.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				claims = append(claims, claim{reminder: row})
				continue
			}
			if err != nil {
				return fmt.Errorf("load active occurrence of %s: %w", formatUUID(row.ID), err)
			}
			claims = append(claims, claim{reminder: row, occurrence: &occurrence})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// deliver sends the message of one claim and persists the outcome.
func (s *Scheduler) deliver(ctx context.Context, c claim, now, lease time.Time) error {
	if c.occurrence == nil {
		return s.fireFirst(ctx, c, now, lease)
	}
	return s.fireRepeat(ctx, c, now, lease)
}

// fireFirst sends the first message of a new occurrence.
//
// A missed schedule is caught up exactly once: whatever the outage was, the user
// gets one message, and the next due time is computed from now rather than from
// the missed instant.
func (s *Scheduler) fireFirst(ctx context.Context, c claim, now, lease time.Time) error {
	// The claim only returns reminders with a due time; a nil one would be a
	// broken invariant, and the lease expiry is the safe way out.
	scheduledAt := timePtr(c.reminder.NextDueAt)
	if scheduledAt == nil {
		return nil
	}
	reminder := reminderFromRow(c.reminder, nil)
	// The next firing is the (fired+1)-th occurrence of the recurrence. A finite
	// rule can have none: the last occurrence is on its way, so next_due_at
	// becomes NULL now and the rule is marked finished after the confirmation.
	nextDue, nextErr := reminder.recurrence().NextDue(now, reminder.OccurrencesFired+1)
	var nextDueAt pgtype.Timestamptz
	switch {
	case errors.Is(nextErr, schedule.ErrNoOccurrence):
	case nextErr != nil:
		return s.recordFailure(ctx, c, now, lease, fmt.Errorf("compute next due time: %w", nextErr))
	default:
		nextDueAt = timestamp(&nextDue)
	}

	occurrenceID, err := newOccurrenceID()
	if err != nil {
		return s.recordFailure(ctx, c, now, lease, err)
	}
	sent, err := s.send(ctx, c.reminder, func(recipient string) unibot.Message {
		return reminderMessage(reminder, recipient, *scheduledAt, 0, formatUUID(occurrenceID))
	})
	if err != nil {
		return s.recordFailure(ctx, c, now, lease, err)
	}
	encoded, err := encodeDeliveries(sent)
	if err != nil {
		return s.recordFailure(ctx, c, now, lease, err)
	}

	nextRepeat := now.Add(repeatInterval(reminder.RepeatMinutes))
	err = s.service.st.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.FinishReminderFirstFire(ctx, db.FinishReminderFirstFireParams{
			NextDueAt:      nextDueAt,
			Now:            now,
			ReminderID:     c.reminder.ID,
			LockUntil:      lease,
			UnchangedSince: c.reminder.UpdatedAt,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrLeaseLost
			}
			return fmt.Errorf("reschedule reminder: %w", err)
		}
		if _, err := q.CreateFiredOccurrence(ctx, db.CreateFiredOccurrenceParams{
			ID:           occurrenceID,
			ReminderID:   c.reminder.ID,
			ScheduledAt:  *scheduledAt,
			FirstSentAt:  timestamp(&now),
			NextRepeatAt: timestamp(&nextRepeat),
			Deliveries:   encoded,
		}); err != nil {
			return fmt.Errorf("create occurrence: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist first fire of %s: %w", formatUUID(c.reminder.ID), err)
	}
	return nil
}

// fireRepeat sends another copy of an unconfirmed reminder. The previous message
// is deleted after the new one is stored, so a failure at any point leaves at
// least one message the user can press.
func (s *Scheduler) fireRepeat(ctx context.Context, c claim, now, lease time.Time) error {
	reminder := reminderFromRow(c.reminder, c.occurrence)
	// repeat_count counts every message including the first one, so the number
	// shown in the message is the count of already sent repeats.
	repeats := int(c.occurrence.RepeatCount)
	occurrenceID := c.occurrence.ID
	sent, err := s.send(ctx, c.reminder, func(recipient string) unibot.Message {
		return reminderMessage(reminder, recipient, c.occurrence.ScheduledAt, repeats, formatUUID(occurrenceID))
	})
	if err != nil {
		return s.recordFailure(ctx, c, now, lease, err)
	}
	encoded, err := encodeDeliveries(sent)
	if err != nil {
		return s.recordFailure(ctx, c, now, lease, err)
	}

	nextRepeat := now.Add(repeatInterval(reminder.RepeatMinutes))
	stale := false
	err = s.service.st.InTx(ctx, func(q *db.Queries) error {
		_, err := q.RecordOccurrenceRepeat(ctx, db.RecordOccurrenceRepeatParams{
			ID:           occurrenceID,
			NextRepeatAt: timestamp(&nextRepeat),
			Deliveries:   encoded,
			UpdatedAt:    now,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The occurrence was confirmed or cancelled while the message was
			// in flight: the newer state wins and this repeat is dropped.
			stale = true
		case err != nil:
			return fmt.Errorf("record repeat: %w", err)
		}
		if _, err := q.FinishReminderRepeat(ctx, db.FinishReminderRepeatParams{
			Now:        now,
			ReminderID: c.reminder.ID,
			LockUntil:  lease,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrLeaseLost
			}
			return fmt.Errorf("release lease: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist repeat of %s: %w", formatUUID(c.reminder.ID), err)
	}

	if stale {
		// The occurrence is over, so the message just sent is noise: remove it
		// and leave the confirmed (edited) message alone.
		s.deleteMessages(ctx, sent, formatUUID(c.reminder.ID))
		return nil
	}
	previous, err := decodeDeliveries(c.occurrence.Deliveries)
	if err != nil {
		s.logger.WarnContext(ctx, "previous reminder message is unknown",
			slog.String("reminder_id", formatUUID(c.reminder.ID)),
			slog.String("error", err.Error()),
		)
		return nil
	}
	s.deleteMessages(ctx, previous, formatUUID(c.reminder.ID))
	return nil
}

// send delivers a firing to every recipient. One reachable recipient is enough
// for the firing to count: retrying it would send the others a second copy.
// Only when nobody got it is the failure returned.
func (s *Scheduler) send(ctx context.Context, reminder db.Reminder, message func(recipient string) unibot.Message) ([]unibot.SentMessage, error) {
	targets, err := deliveryTargets(ctx, s.service.st.Queries(), reminder)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("reminder has no recipients")
	}
	var sent []unibot.SentMessage
	var failures error
	for _, target := range targets {
		delivered, err := s.service.delivery.Send(ctx, target, message(target))
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		sent = append(sent, delivered...)
	}
	if len(sent) == 0 && failures != nil {
		return nil, failures
	}
	if failures != nil {
		s.logger.WarnContext(ctx, "reminder reached only some recipients",
			slog.String("reminder_id", formatUUID(reminder.ID)),
			slog.String("error", failures.Error()),
		)
	}
	return sent, nil
}

// recordFailure stores why a delivery failed, releases the lease and leaves the
// schedule untouched: the next tick retries the same firing.
func (s *Scheduler) recordFailure(ctx context.Context, c claim, now, lease time.Time, cause error) error {
	text := cause.Error()
	if len(text) > maxErrorMessage {
		text = text[:maxErrorMessage]
	}
	released, err := s.service.st.Queries().ReleaseReminderLease(ctx, db.ReleaseReminderLeaseParams{
		ReminderID: c.reminder.ID,
		LastError:  &text,
		Now:        now,
		LockUntil:  lease,
	})
	if err != nil {
		return fmt.Errorf("release lease of %s: %w", formatUUID(c.reminder.ID), err)
	}
	if released == 0 {
		return ErrLeaseLost
	}
	s.logger.WarnContext(ctx, "reminder delivery failed",
		slog.String("reminder_id", formatUUID(c.reminder.ID)),
		slog.String("error", text),
	)
	return nil
}

// deleteMessages removes already delivered messages. Deleting is best effort:
// the message may be gone, and the state change that made it obsolete has
// already happened, so a failure is only logged.
func (s *Scheduler) deleteMessages(ctx context.Context, sent []unibot.SentMessage, reminderID string) {
	if len(sent) == 0 {
		return
	}
	if err := s.service.delivery.Delete(ctx, sent); err != nil {
		s.logger.WarnContext(ctx, "delete previous reminder message failed",
			slog.String("reminder_id", reminderID),
			slog.String("error", err.Error()),
		)
	}
}

// repeatInterval is the pause between two messages of one occurrence.
func repeatInterval(minutes int) time.Duration {
	if minutes < MinRepeatMinutes {
		minutes = MinRepeatMinutes
	}
	return time.Duration(minutes) * time.Minute
}

// newOccurrenceID returns a random version 4 UUID for an occurrence that is
// about to be sent. The id must exist before the row does: the «✅ Выполнено»
// button carries it in its payload while the message travels.
func newOccurrenceID() (pgtype.UUID, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate occurrence id: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return pgtype.UUID{Bytes: bytes, Valid: true}, nil
}
