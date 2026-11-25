// Package reminders implements the reminders feature: the stored rule (when to
// say what), its occurrences (one firing each) and the operations the bot, the
// HTTP API and MCP share.
//
// All data access is scoped by the authenticated principal: every query carries
// the owner id, so a reminder of another user is indistinguishable from an
// unknown one.
package reminders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// Limits of the reminder rule. They are part of the contract: the OpenAPI
// schemas and the bot validation use the same numbers.
const (
	// MaxMessageRunes bounds the reminder text, counted in runes.
	MaxMessageRunes = 1500
	// MinRepeatMinutes and MaxRepeatMinutes bound the repeat interval.
	MinRepeatMinutes = 1
	MaxRepeatMinutes = 1440
	// DefaultRepeatMinutes is used when a client omits the repeat interval.
	DefaultRepeatMinutes = 10
	// MinInterval and MaxInterval bound "every N days/weeks/months/years".
	MinInterval = 1
	MaxInterval = 1000
	// MinEndCount and MaxEndCount bound "after N occurrences".
	MinEndCount = 1
	MaxEndCount = 10000
)

// Kind is the schedule shape, re-exported for the adapters so they do not need
// to import the schedule package.
type Kind = schedule.Kind

// EndKind is the stop condition of a recurrence, re-exported for adapters.
type EndKind = schedule.EndKind

// Date is a civil calendar date, re-exported for adapters.
type Date = schedule.Date

const (
	// EndNever repeats forever.
	EndNever = schedule.EndNever
	// EndUntil repeats through an inclusive end date.
	EndUntil = schedule.EndUntil
	// EndCount repeats a fixed number of times.
	EndCount = schedule.EndCount
)

const (
	// Once is a single occurrence on the start date.
	Once = schedule.Once
	// Daily repeats every Interval days.
	Daily = schedule.Daily
	// Weekly repeats on ISO weekdays (1 = Monday, 7 = Sunday).
	Weekly = schedule.Weekly
	// Monthly repeats on days of month (a missing day means the last day) or on
	// the nth weekday.
	Monthly = schedule.Monthly
	// Yearly repeats on the month and day of the start date.
	Yearly = schedule.Yearly
)

// OccurrenceStatus is the state of one firing.
type OccurrenceStatus string

const (
	// OccurrenceActive means the message was sent and waits for confirmation.
	OccurrenceActive OccurrenceStatus = "active"
	// OccurrenceCompleted means the user confirmed it.
	OccurrenceCompleted OccurrenceStatus = "completed"
	// OccurrenceCancelled means the rule changed or was disabled first.
	OccurrenceCancelled OccurrenceStatus = "cancelled"
)

// Errors that callers translate into protocol-specific answers.
var (
	// ErrNotFound means the reminder does not exist or belongs to another user.
	ErrNotFound = errors.New("reminders: not found")
	// ErrNoActiveOccurrence means there is nothing to confirm.
	ErrNoActiveOccurrence = errors.New("reminders: no active occurrence")
)

// ValidationError is a rejected input. The message is user-facing and Russian,
// so the HTTP API and the bot can show it as is.
type ValidationError struct {
	// Message explains what is wrong, in Russian.
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func validationErrorf(format string, args ...any) *ValidationError {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Reminder is a stored rule together with its pending occurrence, if any.
type Reminder struct {
	// ID is the reminder UUID.
	ID string
	// SpaceID is the reminders space the rule lives in; every member of the
	// space can see and change it.
	SpaceID string
	// AuthorID is the user who created it.
	AuthorID string
	// Recipients are the members of the space who get the message.
	Recipients []string
	// Message is the text the user receives.
	Message string
	// Timezone is the IANA time zone the schedule is interpreted in.
	Timezone string
	// Kind is once, daily, weekly, monthly or yearly.
	Kind schedule.Kind
	// Days are ISO weekdays (1..7) for weekly, or days of month (1..31) for a
	// monthly day-of-month rule.
	Days []int
	// Interval is the gap in kind units: every N days, weeks, months or years.
	Interval int
	// MonthWeekPosition selects the nth weekday of a monthly rule: 1..4, or -1
	// for the last one. Zero means the rule uses Days.
	MonthWeekPosition int
	// MonthWeekday is the ISO weekday of an nth-weekday monthly rule.
	MonthWeekday int
	// StartDate is the first possible day of the rule in Timezone.
	StartDate schedule.Date
	// End is the stop condition: never, until a date, or after N occurrences.
	End schedule.End
	// TimeHHMM is the local wall clock time, HH:MM.
	TimeHHMM string
	// RepeatMinutes is how often an unconfirmed occurrence repeats.
	RepeatMinutes int
	// Enabled reports whether the reminder is scheduled.
	Enabled bool
	// NextDueAt is the next firing in UTC; nil while the reminder is disabled or
	// has no future occurrence.
	NextDueAt *time.Time
	// LastError is the last delivery error, cleared after a successful send.
	LastError *string
	// OccurrencesFired is how many occurrences already fired; an end count uses
	// it to decide when the recurrence is over.
	OccurrencesFired int
	// FinishedAt is when the last occurrence of a finite recurrence was
	// confirmed; nil for a reminder that still has a future occurrence.
	FinishedAt *time.Time
	// CreatedAt and UpdatedAt are the row timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
	// ActiveOccurrence is the firing that waits for confirmation, if any.
	ActiveOccurrence *Occurrence
}

// Finished reports whether a finite recurrence is over: a one-time reminder or
// one whose end date or occurrence count was reached and confirmed.
func (r Reminder) Finished() bool { return r.FinishedAt != nil }

// Occurrence is one firing of a reminder.
type Occurrence struct {
	// ID is the occurrence UUID.
	ID string
	// ReminderID is the reminder that fired.
	ReminderID string
	// ScheduledAt is when it was due, in UTC.
	ScheduledAt time.Time
	// FirstSentAt is when the first message went out; nil before delivery.
	FirstSentAt *time.Time
	// RepeatCount is how many reminder messages were sent.
	RepeatCount int
	// NextRepeatAt is when the next repeat is due, in UTC.
	NextRepeatAt *time.Time
	// Status is active, completed or cancelled.
	Status OccurrenceStatus
	// CompletedAt is when the user confirmed it.
	CompletedAt *time.Time
}

// Input is the full set of rule fields a client supplies to create or replace a
// reminder. Update takes the same struct because PUT replaces the whole rule.
//
// The old clients only know Kind+Days; the rest is optional. An empty StartDate
// means "today in the reminder's time zone", an empty EndKind means "never" and
// an Interval of 0 means 1, so a request that predates the flexible recurrence
// keeps its exact old meaning.
type Input struct {
	// SpaceID names the reminders space. Empty means the default («Личное»)
	// space on create and "do not move" on update.
	SpaceID string
	// Recipients are user ids of members of the space. Nil means the author on
	// create and "keep" on update.
	Recipients        []string
	Message           string
	Timezone          string
	Kind              schedule.Kind
	Days              []int
	Interval          int
	MonthWeekPosition int
	MonthWeekday      int
	StartDate         string
	EndKind           schedule.EndKind
	EndDate           string
	EndCount          int
	TimeHHMM          string
	RepeatMinutes     int
	Enabled           bool
}

// Service is the reminders business logic. Every method takes the principal id
// of the authenticated user and only returns reminders of spaces they are a
// member of.
type Service struct {
	st       *store.Store
	logger   *slog.Logger
	clock    func() time.Time
	delivery Delivery
}

// API is the small surface every adapter uses: the HTTP handlers now, the bot
// and MCP tools in the following stories. It is expressed in plain Go types, so
// no transport type leaks into the business logic, and an adapter cannot grow
// its own copy of the rules.
type API interface {
	// Create stores a new reminder and computes its next due time.
	Create(ctx context.Context, principalID string, input Input) (Reminder, error)
	// List returns the reminders of every space of the principal.
	List(ctx context.Context, principalID string) ([]Reminder, error)
	// ListSpace is List narrowed to one space; empty means every space.
	ListSpace(ctx context.Context, principalID, spaceID string) ([]Reminder, error)
	// Get returns one reminder of the principal.
	Get(ctx context.Context, principalID, id string) (Reminder, error)
	// Update replaces the whole rule of a reminder.
	Update(ctx context.Context, principalID, id string, input Input) (Reminder, error)
	// Delete removes a reminder and its occurrences.
	Delete(ctx context.Context, principalID, id string) error
	// Complete confirms the pending occurrence of a reminder.
	Complete(ctx context.Context, principalID, id string) (Reminder, error)
}

// Service implements API.
var _ API = (*Service)(nil)

// Option customises the service.
type Option func(*Service)

// WithClock injects the time source. The scheduler tests rely on it; production
// uses time.Now.
func WithClock(clock func() time.Time) Option {
	return func(s *Service) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Service) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithDelivery sets the messenger delivery. Without one the scheduler does not
// fire and a completion cannot edit the chat message; the HTTP API and the bot
// commands still work.
func WithDelivery(delivery Delivery) Option {
	return func(s *Service) { s.delivery = delivery }
}

// New returns a service backed by st. A clock may be injected with WithClock.
func New(st *store.Store, opts ...Option) *Service {
	s := &Service{
		st:     st,
		logger: slog.Default(),
		clock:  func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// now is the current instant in UTC, the single source of time for the service.
func (s *Service) now() time.Time { return s.clock().UTC() }

// Create stores a new reminder and computes its next due time when it is
// enabled.
func (s *Service) Create(ctx context.Context, principalID string, input Input) (Reminder, error) {
	var created Reminder
	err := s.st.InTx(ctx, func(q *db.Queries) error {
		var err error
		created, err = s.CreateTx(ctx, q, principalID, input)
		return err
	})
	return created, err
}

// CreateTx is Create over an existing query set, so a reminder can be created
// inside a transaction another caller already opened. The legacy import claim
// uses it: claiming the waiting rows and creating their reminders must happen
// in one transaction, and it must not grow a second copy of the insert. Inside
// somebody else's transaction q is already transactional; Create opens one.
func (s *Service) CreateTx(ctx context.Context, q *db.Queries, principalID string, input Input) (Reminder, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return Reminder{}, err
	}
	rule, err := s.normalize(input)
	if err != nil {
		return Reminder{}, err
	}
	spaceID, err := resolveSpace(ctx, q, userID, input.SpaceID)
	if err != nil {
		return Reminder{}, err
	}
	recipients := input.Recipients
	if recipients == nil {
		recipients = []string{principalID}
	}
	row, err := q.CreateReminder(ctx, db.CreateReminderParams{
		WorkspaceID:       spaceID,
		UserID:            userID,
		Message:           rule.message,
		Timezone:          rule.timezone,
		ScheduleKind:      string(rule.kind),
		ScheduleDays:      daysToInt16(rule.days),
		TimeHhmm:          rule.hhmm,
		RepeatMinutes:     int32(rule.repeatMinutes),
		Enabled:           rule.enabled,
		NextDueAt:         timestamp(rule.nextDueAt),
		IntervalCount:     int32(rule.interval),
		MonthWeekPosition: optionalInt16(rule.monthWeekPosition),
		MonthWeekday:      optionalInt16(rule.monthWeekday),
		StartDate:         dateToPg(rule.startDate),
		EndKind:           string(rule.end.Kind),
		EndDate:           dateToPg(rule.end.Date),
		EndCount:          optionalInt32(rule.end.Count),
	})
	if err != nil {
		return Reminder{}, fmt.Errorf("create reminder: %w", err)
	}
	created := reminderFromRow(row, nil)
	if created.Recipients, err = setRecipients(ctx, q, row, recipients); err != nil {
		return Reminder{}, err
	}
	return created, nil
}

// List returns the reminders of every space of the principal, each with its
// pending occurrence.
func (s *Service) List(ctx context.Context, principalID string) ([]Reminder, error) {
	return s.ListSpace(ctx, principalID, "")
}

// ListSpace is List narrowed to one space; an empty id means every space. A
// space the principal is not a member of answers with an empty list.
func (s *Service) ListSpace(ctx context.Context, principalID, spaceID string) ([]Reminder, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return nil, err
	}
	var space pgtype.UUID
	if spaceID != "" {
		if space, err = workspace.ParseID(spaceID); err != nil {
			return nil, ErrSpaceNotFound
		}
	}
	rows, err := s.st.Queries().ListReminders(ctx, db.ListRemindersParams{UserID: userID, WorkspaceID: space})
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	occurrences, err := s.st.Queries().ListActiveOccurrencesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list active occurrences: %w", err)
	}
	active := make(map[string]*db.ReminderOccurrence, len(occurrences))
	for i := range occurrences {
		active[formatUUID(occurrences[i].ReminderID)] = &occurrences[i]
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	recipients, err := recipientsOf(ctx, s.st.Queries(), ids...)
	if err != nil {
		return nil, err
	}
	out := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		reminder := reminderFromRow(row, active[formatUUID(row.ID)])
		reminder.Recipients = recipients[reminder.ID]
		out = append(out, reminder)
	}
	return out, nil
}

// Get returns one reminder of the principal. A reminder of another user is
// reported as ErrNotFound.
func (s *Service) Get(ctx context.Context, principalID, id string) (Reminder, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return Reminder{}, err
	}
	reminderID, err := parseReminderID(id)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.st.Queries().GetReminder(ctx, db.GetReminderParams{ID: reminderID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("get reminder: %w", err)
	}
	active, err := s.activeOccurrence(ctx, reminderID)
	if err != nil {
		return Reminder{}, err
	}
	reminder := reminderFromRow(row, active)
	recipients, err := recipientsOf(ctx, s.st.Queries(), reminderID)
	if err != nil {
		return Reminder{}, err
	}
	reminder.Recipients = recipients[reminder.ID]
	return reminder, nil
}

// Update replaces the whole rule (PUT semantics). The pending occurrence is
// cancelled and next_due_at is recomputed from the current time; a disabled
// reminder loses its next due time.
func (s *Service) Update(ctx context.Context, principalID, id string, input Input) (Reminder, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return Reminder{}, err
	}
	reminderID, err := parseReminderID(id)
	if err != nil {
		return Reminder{}, err
	}
	rule, err := s.normalize(input)
	if err != nil {
		return Reminder{}, err
	}

	var updated db.Reminder
	var recipients []string
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		current, err := q.GetReminder(ctx, db.GetReminderParams{ID: reminderID, UserID: userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get reminder: %w", err)
		}
		spaceID := current.WorkspaceID
		if input.SpaceID != "" {
			if spaceID, err = resolveSpace(ctx, q, userID, input.SpaceID); err != nil {
				return err
			}
		}
		row, err := q.UpdateReminder(ctx, db.UpdateReminderParams{
			WorkspaceID:       spaceID,
			ID:                reminderID,
			UserID:            userID,
			Message:           rule.message,
			Timezone:          rule.timezone,
			ScheduleKind:      string(rule.kind),
			ScheduleDays:      daysToInt16(rule.days),
			TimeHhmm:          rule.hhmm,
			RepeatMinutes:     int32(rule.repeatMinutes),
			Enabled:           rule.enabled,
			NextDueAt:         timestamp(rule.nextDueAt),
			IntervalCount:     int32(rule.interval),
			MonthWeekPosition: optionalInt16(rule.monthWeekPosition),
			MonthWeekday:      optionalInt16(rule.monthWeekday),
			StartDate:         dateToPg(rule.startDate),
			EndKind:           string(rule.end.Kind),
			EndDate:           dateToPg(rule.end.Date),
			EndCount:          optionalInt32(rule.end.Count),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("update reminder: %w", err)
		}
		// The old message must not repeat: the user asked for another rule.
		if _, err := q.CancelActiveOccurrence(ctx, reminderID); err != nil {
			return fmt.Errorf("cancel active occurrence: %w", err)
		}
		updated = row

		wanted := input.Recipients
		if wanted == nil {
			// Keeping the recipients across a move drops those who are not
			// members of the new space.
			kept, err := recipientsOf(ctx, q, reminderID)
			if err != nil {
				return err
			}
			wanted = kept[formatUUID(reminderID)]
			if len(wanted) == 0 {
				wanted = []string{principalID}
			}
		}
		recipients, err = setRecipients(ctx, q, row, wanted)
		return err
	})
	if err != nil {
		return Reminder{}, err
	}
	reminder := reminderFromRow(updated, nil)
	reminder.Recipients = recipients
	return reminder, nil
}

// Delete removes a reminder; its occurrences go with it through the foreign key.
func (s *Service) Delete(ctx context.Context, principalID, id string) error {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return err
	}
	reminderID, err := parseReminderID(id)
	if err != nil {
		return err
	}
	deleted, err := s.st.Queries().DeleteReminder(ctx, db.DeleteReminderParams{ID: reminderID, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

// CompletionResult is the outcome of a confirmation request. The bot turns it
// into the answer shown to the user; the HTTP API keeps its older contract
// (404/409/200), so both entry points share one state change but keep their own
// error shapes.
type CompletionResult string

const (
	// CompletionCompleted means the occurrence was active and is now completed.
	CompletionCompleted CompletionResult = "completed"
	// CompletionAlready means it had already been confirmed.
	CompletionAlready CompletionResult = "already"
	// CompletionStale means the rule changed or was disabled: the occurrence is
	// no longer actual.
	CompletionStale CompletionResult = "stale"
	// CompletionNotOwner means the reminder belongs to another user.
	CompletionNotOwner CompletionResult = "not_owner"
	// CompletionNotFound means the reminder or the occurrence is gone.
	CompletionNotFound CompletionResult = "not_found"
)

// Complete confirms the pending occurrence of a reminder. ErrNoActiveOccurrence
// means there was nothing to confirm, for example because the occurrence was
// already completed or the rule changed.
func (s *Service) Complete(ctx context.Context, principalID, id string) (Reminder, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return Reminder{}, err
	}
	reminderID, err := parseReminderID(id)
	if err != nil {
		return Reminder{}, err
	}
	// Ownership is checked first so a foreign reminder answers ErrNotFound
	// instead of "nothing to confirm".
	if _, err := s.st.Queries().GetReminder(ctx, db.GetReminderParams{ID: reminderID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reminder{}, ErrNotFound
		}
		return Reminder{}, fmt.Errorf("get reminder: %w", err)
	}
	active, err := s.activeOccurrence(ctx, reminderID)
	if err != nil {
		return Reminder{}, err
	}
	if active == nil {
		return Reminder{}, ErrNoActiveOccurrence
	}

	// The confirmation itself is the same atomic statement the chat button uses.
	result, err := s.completeOccurrence(ctx, userID, reminderID, active.ID)
	if err != nil {
		return Reminder{}, err
	}
	switch result {
	case CompletionCompleted:
		return s.Get(ctx, principalID, id)
	case CompletionNotFound, CompletionNotOwner:
		return Reminder{}, ErrNotFound
	default:
		// Another entry point won the race: there is nothing left to confirm.
		return Reminder{}, ErrNoActiveOccurrence
	}
}

// CompleteOccurrence confirms one specific occurrence of a reminder, exactly as
// the «✅ Выполнено» button asks. The state change is a single guarded UPDATE, so
// the chat, the HTTP API and MCP can race and only one of them wins.
//
// The typed result is not an error: every outcome except an infrastructure
// failure has a user-facing answer.
func (s *Service) CompleteOccurrence(ctx context.Context, principalID, reminderID, occurrenceID string) (CompletionResult, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return CompletionNotFound, err
	}
	parsedReminder, err := parseReminderID(reminderID)
	if err != nil {
		return CompletionNotFound, nil
	}
	parsedOccurrence, err := parseOccurrenceID(occurrenceID)
	if err != nil {
		return CompletionNotFound, nil
	}
	return s.completeOccurrence(ctx, userID, parsedReminder, parsedOccurrence)
}

// completeOccurrence is the shared implementation. It classifies the outcome
// before the update (so the caller can answer "already", "stale" or "not your
// reminder") and then performs the guarded update that decides the winner.
func (s *Service) completeOccurrence(ctx context.Context, userID, reminderID, occurrenceID pgtype.UUID) (CompletionResult, error) {
	// Access is read without the membership filter, because the answer differs
	// between a foreign reminder and a vanishing one.
	allowed, err := s.st.Queries().CanAccessReminder(ctx, db.CanAccessReminderParams{ID: reminderID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionNotFound, nil
	}
	if err != nil {
		return CompletionNotFound, fmt.Errorf("check reminder access: %w", err)
	}
	if !allowed {
		return CompletionNotOwner, nil
	}

	occurrence, err := s.st.Queries().GetOccurrence(ctx, db.GetOccurrenceParams{
		ID:         occurrenceID,
		ReminderID: reminderID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return CompletionNotFound, nil
	case err != nil:
		return CompletionNotFound, fmt.Errorf("get occurrence: %w", err)
	case OccurrenceStatus(occurrence.Status) == OccurrenceCompleted:
		return CompletionAlready, nil
	case OccurrenceStatus(occurrence.Status) != OccurrenceActive:
		return CompletionStale, nil
	}

	completedAt := s.now()
	completed, err := s.st.Queries().CompleteOccurrence(ctx, db.CompleteOccurrenceParams{
		CompletedAt:  completedAt,
		OccurrenceID: occurrenceID,
		ReminderID:   reminderID,
		UserID:       userID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Somebody else completed or cancelled it between the lookup and the
		// update: report the state that actually won.
		return s.classifyOccurrence(ctx, reminderID, occurrenceID)
	case err != nil:
		return CompletionNotFound, fmt.Errorf("complete occurrence: %w", err)
	}

	row, err := s.st.Queries().GetReminder(ctx, db.GetReminderParams{ID: reminderID, UserID: userID})
	if err != nil {
		s.logger.ErrorContext(ctx, "load reminder after completion failed",
			slog.String("reminder_id", formatUUID(reminderID)),
			slog.String("error", err.Error()),
		)
		return CompletionCompleted, nil
	}
	reminder := reminderFromRow(row, nil)
	// A finite recurrence ends with the confirmation of its last occurrence:
	// from now on the rule has no future occurrence, so it becomes «завершено»
	// instead of looking like a user-disabled reminder.
	if _, nextErr := reminder.recurrence().NextDue(completedAt, reminder.OccurrencesFired); errors.Is(nextErr, schedule.ErrNoOccurrence) {
		finished, finishErr := s.st.Queries().FinishReminder(ctx, db.FinishReminderParams{
			FinishedAt: completedAt,
			ReminderID: reminderID,
		})
		switch {
		case finishErr == nil:
			reminder = reminderFromRow(finished, nil)
		case errors.Is(finishErr, pgx.ErrNoRows):
			// The reminder disappeared between the confirmation and this
			// update; there is nothing left to show.
		default:
			s.logger.ErrorContext(ctx, "mark reminder finished failed",
				slog.String("reminder_id", formatUUID(reminderID)),
				slog.String("error", finishErr.Error()),
			)
		}
	}
	s.editCompleted(ctx, reminder, occurrenceFromRow(completed), completed.Deliveries, completedAt)
	return CompletionCompleted, nil
}

// classifyOccurrence re-reads an occurrence that lost a race and maps its state
// to a result.
func (s *Service) classifyOccurrence(ctx context.Context, reminderID, occurrenceID pgtype.UUID) (CompletionResult, error) {
	occurrence, err := s.st.Queries().GetOccurrence(ctx, db.GetOccurrenceParams{
		ID:         occurrenceID,
		ReminderID: reminderID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return CompletionNotFound, nil
	case err != nil:
		return CompletionNotFound, fmt.Errorf("get occurrence: %w", err)
	case OccurrenceStatus(occurrence.Status) == OccurrenceCompleted:
		return CompletionAlready, nil
	default:
		return CompletionStale, nil
	}
}

// editCompleted rewrites the delivered messages into the completion text with
// the button removed. It is best effort: the confirmation is already stored, and
// a messenger problem must not undo it.
func (s *Service) editCompleted(ctx context.Context, reminder Reminder, occurrence Occurrence, deliveries []byte, completedAt time.Time) {
	if s.delivery == nil {
		return
	}
	sent, err := decodeDeliveries(deliveries)
	if err != nil {
		s.logger.WarnContext(ctx, "delivered messages of the occurrence are unknown",
			slog.String("reminder_id", reminder.ID),
			slog.String("occurrence_id", occurrence.ID),
			slog.String("error", err.Error()),
		)
		return
	}
	if len(sent) == 0 {
		return
	}
	if err := s.delivery.Edit(ctx, sent, completedMessage(reminder, occurrence, completedAt)); err != nil {
		s.logger.WarnContext(ctx, "update completed reminder message failed",
			slog.String("reminder_id", reminder.ID),
			slog.String("occurrence_id", occurrence.ID),
			slog.String("error", err.Error()),
		)
	}
}

// activeOccurrence loads the pending occurrence of a reminder. Missing is not an
// error: most reminders have no pending occurrence.
func (s *Service) activeOccurrence(ctx context.Context, reminderID pgtype.UUID) (*db.ReminderOccurrence, error) {
	row, err := s.st.Queries().GetActiveOccurrence(ctx, reminderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active occurrence: %w", err)
	}
	return &row, nil
}

// rule is the validated, canonical form of an Input together with the computed
// next due time.
type rule struct {
	message           string
	timezone          string
	kind              schedule.Kind
	days              []int
	interval          int
	monthWeekPosition int
	monthWeekday      int
	startDate         schedule.Date
	end               schedule.End
	hhmm              string
	repeatMinutes     int
	enabled           bool
	nextDueAt         *time.Time
}

// recurrence turns the rule into the pure description the schedule package
// computes with.
func (r rule) recurrence() schedule.Recurrence {
	return schedule.Recurrence{
		Kind:              r.kind,
		Interval:          r.interval,
		Days:              r.days,
		MonthWeekPosition: r.monthWeekPosition,
		MonthWeekday:      r.monthWeekday,
		StartDate:         r.startDate,
		End:               r.end,
		TimeHHMM:          r.hhmm,
		Timezone:          r.timezone,
	}
}

// recurrence is the schedule description of a stored reminder.
func (r Reminder) recurrence() schedule.Recurrence {
	return schedule.Recurrence{
		Kind:              r.Kind,
		Interval:          r.Interval,
		Days:              r.Days,
		MonthWeekPosition: r.MonthWeekPosition,
		MonthWeekday:      r.MonthWeekday,
		StartDate:         r.StartDate,
		End:               r.End,
		TimeHHMM:          r.TimeHHMM,
		Timezone:          r.Timezone,
	}
}

// normalize validates the input and computes next_due_at. Everything that
// reaches the database is canonical: trimmed message, sorted days, fixed-width
// time.
func (s *Service) normalize(input Input) (rule, error) {
	now := s.now()
	validated, err := canonicalizeAt(input, now)
	if err != nil {
		return rule{}, err
	}
	if !validated.enabled {
		return validated, nil
	}
	next, err := validated.recurrence().NextDue(now, 0)
	if errors.Is(err, schedule.ErrNoOccurrence) {
		return rule{}, validationErrorf("В расписании нет будущих срабатываний")
	}
	if err != nil {
		return rule{}, fmt.Errorf("compute next due time: %w", err)
	}
	validated.nextDueAt = &next
	return validated, nil
}

// canonicalize validates the input without a clock. It is used where the actual
// instant does not matter (parsing an export); the service always uses the
// clock-aware form so "today" is the user's today.
func canonicalize(input Input) (rule, error) {
	return canonicalizeAt(input, time.Now().UTC())
}

// canonicalizeAt validates the input and returns its canonical form without the
// next due time, which depends on the current instant. It is the single copy of
// the validation rules: the service, the bot form and the legacy import all go
// through it.
func canonicalizeAt(input Input, now time.Time) (rule, error) {
	message := strings.TrimSpace(input.Message)
	if length := utf8.RuneCountInString(message); length < 1 || length > MaxMessageRunes {
		return rule{}, validationErrorf("Сообщение должно быть от 1 до %d символов", MaxMessageRunes)
	}

	timezone := strings.TrimSpace(input.Timezone)
	location, err := schedule.LoadLocation(timezone)
	if err != nil {
		return rule{}, validationErrorf("Неизвестный часовой пояс")
	}

	hhmm := strings.TrimSpace(input.TimeHHMM)
	if _, _, err := schedule.ParseHHMM(hhmm); err != nil {
		return rule{}, validationErrorf("Время должно быть в формате ЧЧ:ММ")
	}

	interval := input.Interval
	if interval == 0 {
		interval = 1
	}
	if interval < MinInterval || interval > MaxInterval {
		return rule{}, validationErrorf("Интервал должен быть от %d до %d", MinInterval, MaxInterval)
	}

	startDate := schedule.DateOf(now, location)
	if value := strings.TrimSpace(input.StartDate); value != "" {
		parsed, err := schedule.ParseDate(value)
		if err != nil {
			return rule{}, validationErrorf("Дата начала должна быть в формате ГГГГ-ММ-ДД")
		}
		startDate = parsed
	}

	days := schedule.NormalizeDays(input.Days)
	monthWeekPosition := input.MonthWeekPosition
	monthWeekday := input.MonthWeekday

	switch input.Kind {
	case schedule.Once:
		// A one-time rule has no days of its own: the start date is the date.
		days = nil
		monthWeekPosition, monthWeekday = 0, 0
		interval = 1
	case schedule.Daily, schedule.Yearly:
		days = nil
		monthWeekPosition, monthWeekday = 0, 0
	case schedule.Weekly:
		monthWeekPosition, monthWeekday = 0, 0
		if len(days) == 0 {
			return rule{}, validationErrorf("Укажите хотя бы один день недели")
		}
		if err := schedule.ValidateDays(schedule.Weekly, days); err != nil {
			return rule{}, validationErrorf("Дни недели должны быть от 1 (Пн) до 7 (Вс)")
		}
	case schedule.Monthly:
		if monthWeekPosition == 0 && monthWeekday != 0 {
			return rule{}, validationErrorf("Укажите позицию дня недели в месяце")
		}
		if monthWeekPosition != 0 {
			if len(days) > 0 {
				return rule{}, validationErrorf("Нельзя одновременно указать дни месяца и день недели")
			}
			if monthWeekPosition < -1 || monthWeekPosition == 0 || monthWeekPosition > 4 {
				return rule{}, validationErrorf("Позиция дня недели должна быть от 1 до 4 или последняя")
			}
			if monthWeekday < 1 || monthWeekday > 7 {
				return rule{}, validationErrorf("День недели должен быть от 1 (Пн) до 7 (Вс)")
			}
			days = nil
		} else {
			if len(days) == 0 {
				return rule{}, validationErrorf("Укажите хотя бы один день месяца")
			}
			if err := schedule.ValidateDays(schedule.Monthly, days); err != nil {
				return rule{}, validationErrorf("Дни месяца должны быть от 1 до 31")
			}
		}
	default:
		return rule{}, validationErrorf("Неизвестный тип расписания")
	}

	end := schedule.End{Kind: schedule.EndNever}
	switch input.EndKind {
	case "", schedule.EndNever:
	case schedule.EndUntil:
		value := strings.TrimSpace(input.EndDate)
		if value == "" {
			return rule{}, validationErrorf("Укажите дату окончания")
		}
		endDate, err := schedule.ParseDate(value)
		if err != nil {
			return rule{}, validationErrorf("Дата окончания должна быть в формате ГГГГ-ММ-ДД")
		}
		if endDate.Before(startDate) {
			return rule{}, validationErrorf("Дата окончания не может быть раньше даты начала")
		}
		end = schedule.End{Kind: schedule.EndUntil, Date: endDate}
	case schedule.EndCount:
		if input.EndCount < MinEndCount || input.EndCount > MaxEndCount {
			return rule{}, validationErrorf("Количество повторов должно быть от %d до %d", MinEndCount, MaxEndCount)
		}
		end = schedule.End{Kind: schedule.EndCount, Count: input.EndCount}
	default:
		return rule{}, validationErrorf("Неизвестное условие окончания")
	}
	if input.Kind == schedule.Once {
		end = schedule.End{Kind: schedule.EndNever}
	}

	if input.RepeatMinutes < MinRepeatMinutes || input.RepeatMinutes > MaxRepeatMinutes {
		return rule{}, validationErrorf(
			"Интервал повтора должен быть от %d до %d минут", MinRepeatMinutes, MaxRepeatMinutes,
		)
	}

	return rule{
		message:           message,
		timezone:          timezone,
		kind:              input.Kind,
		days:              days,
		interval:          interval,
		monthWeekPosition: monthWeekPosition,
		monthWeekday:      monthWeekday,
		startDate:         startDate,
		end:               end,
		hhmm:              hhmm,
		repeatMinutes:     input.RepeatMinutes,
		enabled:           input.Enabled,
	}, nil
}

// parsePrincipal turns the authenticated principal id into a UUID. The id comes
// from a session, so a malformed one is a server-side problem, not user input.
func parsePrincipal(principalID string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(principalID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid principal id %q: %w", principalID, err)
	}
	if !id.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid principal id %q", principalID)
	}
	return id, nil
}

// parseReminderID treats a malformed id exactly like an unknown one, so the API
// cannot be used to probe for the existence of reminders.
func parseReminderID(id string) (pgtype.UUID, error) {
	var parsed pgtype.UUID
	if err := parsed.Scan(id); err != nil || !parsed.Valid {
		return pgtype.UUID{}, ErrNotFound
	}
	return parsed, nil
}

// parseOccurrenceID treats a malformed id exactly like an unknown one: the id
// travels as an opaque button payload.
func parseOccurrenceID(id string) (pgtype.UUID, error) {
	var parsed pgtype.UUID
	if err := parsed.Scan(id); err != nil || !parsed.Valid {
		return pgtype.UUID{}, ErrNotFound
	}
	return parsed, nil
}

// formatUUID renders a uuid column in canonical form.
func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// timestamp converts an optional instant to a nullable column value.
func timestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

// timePtr converts a nullable column value to an optional instant in UTC.
func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	utc := value.Time.UTC()
	return &utc
}

func daysToInt16(days []int) []int16 {
	out := make([]int16, 0, len(days))
	for _, day := range days {
		out = append(out, int16(day))
	}
	return out
}

func daysToInt(days []int16) []int {
	out := make([]int, 0, len(days))
	for _, day := range days {
		out = append(out, int(day))
	}
	return out
}

// dateToPg converts a civil date to the date column value. A zero date is NULL.
func dateToPg(value schedule.Date) pgtype.Date {
	if value.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{
		Time:  time.Date(value.Year, value.Month, value.Day, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}
}

// dateFromPg converts a date column to a civil date.
func dateFromPg(value pgtype.Date) schedule.Date {
	if !value.Valid {
		return schedule.Date{}
	}
	return schedule.Date{Year: value.Time.Year(), Month: value.Time.Month(), Day: value.Time.Day()}
}

// optionalInt16 maps a zero to SQL NULL, which keeps "no nth weekday" a single
// representation instead of 0 and NULL.
func optionalInt16(value int) *int16 {
	if value == 0 {
		return nil
	}
	converted := int16(value)
	return &converted
}

// optionalInt32 maps a zero to SQL NULL, used by the end count.
func optionalInt32(value int) *int32 {
	if value == 0 {
		return nil
	}
	converted := int32(value)
	return &converted
}

func intFromOptional(pointer *int16) int {
	if pointer == nil {
		return 0
	}
	return int(*pointer)
}

func int32FromOptional(pointer *int32) int {
	if pointer == nil {
		return 0
	}
	return int(*pointer)
}

func reminderFromRow(row db.Reminder, active *db.ReminderOccurrence) Reminder {
	reminder := Reminder{
		ID:                formatUUID(row.ID),
		SpaceID:           formatUUID(row.WorkspaceID),
		AuthorID:          formatUUID(row.UserID),
		Message:           row.Message,
		Timezone:          row.Timezone,
		Kind:              schedule.Kind(row.ScheduleKind),
		Days:              daysToInt(row.ScheduleDays),
		Interval:          int(row.IntervalCount),
		MonthWeekPosition: intFromOptional(row.MonthWeekPosition),
		MonthWeekday:      intFromOptional(row.MonthWeekday),
		StartDate:         dateFromPg(row.StartDate),
		End: schedule.End{
			Kind:  schedule.EndKind(row.EndKind),
			Date:  dateFromPg(row.EndDate),
			Count: int32FromOptional(row.EndCount),
		},
		TimeHHMM:         row.TimeHhmm,
		RepeatMinutes:    int(row.RepeatMinutes),
		Enabled:          row.Enabled,
		NextDueAt:        timePtr(row.NextDueAt),
		LastError:        row.LastError,
		OccurrencesFired: int(row.OccurrencesFired),
		FinishedAt:       timePtr(row.FinishedAt),
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
	if active != nil {
		occurrence := occurrenceFromRow(*active)
		reminder.ActiveOccurrence = &occurrence
	}
	return reminder
}

func occurrenceFromRow(row db.ReminderOccurrence) Occurrence {
	return Occurrence{
		ID:           formatUUID(row.ID),
		ReminderID:   formatUUID(row.ReminderID),
		ScheduledAt:  row.ScheduledAt.UTC(),
		FirstSentAt:  timePtr(row.FirstSentAt),
		RepeatCount:  int(row.RepeatCount),
		NextRepeatAt: timePtr(row.NextRepeatAt),
		Status:       OccurrenceStatus(row.Status),
		CompletedAt:  timePtr(row.CompletedAt),
	}
}
