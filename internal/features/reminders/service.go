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
)

// Kind is the schedule shape, re-exported for the adapters so they do not need
// to import the schedule package.
type Kind = schedule.Kind

const (
	// Weekly repeats on ISO weekdays (1 = Monday, 7 = Sunday).
	Weekly = schedule.Weekly
	// Monthly repeats on days of month (a missing day means the last day).
	Monthly = schedule.Monthly
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
	// Message is the text the user receives.
	Message string
	// Timezone is the IANA time zone the schedule is interpreted in.
	Timezone string
	// Kind is weekly or monthly.
	Kind schedule.Kind
	// Days are ISO weekdays (1..7) or days of month (1..31), sorted.
	Days []int
	// TimeHHMM is the local wall clock time, HH:MM.
	TimeHHMM string
	// RepeatMinutes is how often an unconfirmed occurrence repeats.
	RepeatMinutes int
	// Enabled reports whether the reminder is scheduled.
	Enabled bool
	// NextDueAt is the next firing in UTC; nil while the reminder is disabled.
	NextDueAt *time.Time
	// LastError is the last delivery error, cleared after a successful send.
	LastError *string
	// CreatedAt and UpdatedAt are the row timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
	// ActiveOccurrence is the firing that waits for confirmation, if any.
	ActiveOccurrence *Occurrence
}

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
type Input struct {
	Message       string
	Timezone      string
	Kind          schedule.Kind
	Days          []int
	TimeHHMM      string
	RepeatMinutes int
	Enabled       bool
}

// Service is the reminders business logic. Every method takes the principal id
// of the authenticated user and never returns data of another user.
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
	// List returns every reminder of the principal.
	List(ctx context.Context, principalID string) ([]Reminder, error)
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
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return Reminder{}, err
	}
	rule, err := s.normalize(input)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.st.Queries().CreateReminder(ctx, db.CreateReminderParams{
		UserID:        userID,
		Message:       rule.message,
		Timezone:      rule.timezone,
		ScheduleKind:  string(rule.kind),
		ScheduleDays:  daysToInt16(rule.days),
		TimeHhmm:      rule.hhmm,
		RepeatMinutes: int32(rule.repeatMinutes),
		Enabled:       rule.enabled,
		NextDueAt:     timestamp(rule.nextDueAt),
	})
	if err != nil {
		return Reminder{}, fmt.Errorf("create reminder: %w", err)
	}
	return reminderFromRow(row, nil), nil
}

// List returns every reminder of the principal, each with its pending
// occurrence.
func (s *Service) List(ctx context.Context, principalID string) ([]Reminder, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListReminders(ctx, userID)
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
	out := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		out = append(out, reminderFromRow(row, active[formatUUID(row.ID)]))
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
	return reminderFromRow(row, active), nil
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
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		row, err := q.UpdateReminder(ctx, db.UpdateReminderParams{
			ID:            reminderID,
			UserID:        userID,
			Message:       rule.message,
			Timezone:      rule.timezone,
			ScheduleKind:  string(rule.kind),
			ScheduleDays:  daysToInt16(rule.days),
			TimeHhmm:      rule.hhmm,
			RepeatMinutes: int32(rule.repeatMinutes),
			Enabled:       rule.enabled,
			NextDueAt:     timestamp(rule.nextDueAt),
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
		return nil
	})
	if err != nil {
		return Reminder{}, err
	}
	return reminderFromRow(updated, nil), nil
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
	// The owner is read without the ownership filter, because the answer differs
	// between a foreign reminder and a vanishing one.
	owner, err := s.st.Queries().GetReminderOwner(ctx, reminderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionNotFound, nil
	}
	if err != nil {
		return CompletionNotFound, fmt.Errorf("get reminder owner: %w", err)
	}
	if owner != userID {
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
	s.editCompleted(ctx, reminderFromRow(row, nil), occurrenceFromRow(completed), completed.Deliveries, completedAt)
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
	message       string
	timezone      string
	kind          schedule.Kind
	days          []int
	hhmm          string
	repeatMinutes int
	enabled       bool
	nextDueAt     *time.Time
}

// normalize validates the input and computes next_due_at. Everything that
// reaches the database is canonical: trimmed message, sorted days, fixed-width
// time.
func (s *Service) normalize(input Input) (rule, error) {
	message := strings.TrimSpace(input.Message)
	if length := utf8.RuneCountInString(message); length < 1 || length > MaxMessageRunes {
		return rule{}, validationErrorf("Сообщение должно быть от 1 до %d символов", MaxMessageRunes)
	}

	timezone := strings.TrimSpace(input.Timezone)
	if _, err := schedule.LoadLocation(timezone); err != nil {
		return rule{}, validationErrorf("Неизвестный часовой пояс")
	}

	hhmm := strings.TrimSpace(input.TimeHHMM)
	if _, _, err := schedule.ParseHHMM(hhmm); err != nil {
		return rule{}, validationErrorf("Время должно быть в формате ЧЧ:ММ")
	}

	days := schedule.NormalizeDays(input.Days)
	switch input.Kind {
	case schedule.Weekly:
		if len(days) == 0 {
			return rule{}, validationErrorf("Укажите хотя бы один день недели")
		}
		if err := schedule.ValidateDays(schedule.Weekly, days); err != nil {
			return rule{}, validationErrorf("Дни недели должны быть от 1 (Пн) до 7 (Вс)")
		}
	case schedule.Monthly:
		if len(days) == 0 {
			return rule{}, validationErrorf("Укажите хотя бы один день месяца")
		}
		if err := schedule.ValidateDays(schedule.Monthly, days); err != nil {
			return rule{}, validationErrorf("Дни месяца должны быть от 1 до 31")
		}
	default:
		return rule{}, validationErrorf("Неизвестный тип расписания")
	}

	if input.RepeatMinutes < MinRepeatMinutes || input.RepeatMinutes > MaxRepeatMinutes {
		return rule{}, validationErrorf(
			"Интервал повтора должен быть от %d до %d минут", MinRepeatMinutes, MaxRepeatMinutes,
		)
	}

	validated := rule{
		message:       message,
		timezone:      timezone,
		kind:          input.Kind,
		days:          days,
		hhmm:          hhmm,
		repeatMinutes: input.RepeatMinutes,
		enabled:       input.Enabled,
	}
	if !validated.enabled {
		return validated, nil
	}
	next, err := schedule.NextDue(s.now(), validated.kind, validated.days, validated.hhmm, validated.timezone)
	if err != nil {
		return rule{}, fmt.Errorf("compute next due time: %w", err)
	}
	validated.nextDueAt = &next
	return validated, nil
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

func reminderFromRow(row db.Reminder, active *db.ReminderOccurrence) Reminder {
	reminder := Reminder{
		ID:            formatUUID(row.ID),
		Message:       row.Message,
		Timezone:      row.Timezone,
		Kind:          schedule.Kind(row.ScheduleKind),
		Days:          daysToInt(row.ScheduleDays),
		TimeHHMM:      row.TimeHhmm,
		RepeatMinutes: int(row.RepeatMinutes),
		Enabled:       row.Enabled,
		NextDueAt:     timePtr(row.NextDueAt),
		LastError:     row.LastError,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
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
