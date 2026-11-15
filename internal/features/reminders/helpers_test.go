package reminders_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// fixedNow is the clock of the integration tests: Monday 2026-01-05 08:00 UTC.
// In Europe/Moscow (UTC+3) it is 11:00 on Monday, so a 09:00 reminder has
// already passed today and a 12:00 one is still ahead.
var fixedNow = time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)

// testEnv is a real service stack: a PostgreSQL container, the migrations and
// the reminders service with a controllable clock.
type testEnv struct {
	t        *testing.T
	store    *store.Store
	service  *reminders.Service
	delivery *fakeDelivery
	now      time.Time
}

// newTestEnv starts PostgreSQL and applies the migrations.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	databaseURL := testdb.Start(t)
	if err := store.Migrate(ctx, databaseURL, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	env := &testEnv{t: t, store: st, delivery: newFakeDelivery(), now: fixedNow}
	env.service = reminders.New(st,
		reminders.WithLogger(logger),
		reminders.WithClock(func() time.Time { return env.now }),
		reminders.WithDelivery(env.delivery),
	)
	return env
}

// scheduler returns a scheduler over the environment service. Tests call Tick
// directly, so nothing depends on wall-clock timing.
func (e *testEnv) scheduler() *reminders.Scheduler {
	return reminders.NewScheduler(e.service, reminders.SchedulerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// dueReminder creates a reminder and moves the clock to its due instant, so the
// next tick fires it.
func (e *testEnv) dueReminder(principal string, input reminders.Input) reminders.Reminder {
	e.t.Helper()
	created, err := e.service.Create(context.Background(), principal, input)
	if err != nil {
		e.t.Fatalf("create due reminder: %v", err)
	}
	if created.NextDueAt == nil {
		e.t.Fatal("a due reminder has no next_due_at")
	}
	e.now = *created.NextDueAt
	return created
}

// advance moves the test clock.
func (e *testEnv) advance(d time.Duration) {
	e.now = e.now.Add(d)
}

// createUser inserts a user row the way an OIDC sign-in would and returns the
// principal id.
func (e *testEnv) createUser(subject string) string {
	e.t.Helper()
	email := subject + "@example.com"
	name := "Пользователь " + subject
	row, err := e.store.Queries().CreateUser(context.Background(), db.CreateUserParams{
		OidcIssuer:  "https://idp.example.com",
		OidcSubject: subject,
		Email:       &email,
		DisplayName: &name,
	})
	if err != nil {
		e.t.Fatalf("create user %q: %v", subject, err)
	}
	return formatTestUUID(row.ID)
}

// createOccurrence stores an active occurrence, which is what the scheduler does
// when a reminder fires.
func (e *testEnv) createOccurrence(reminderID string, scheduledAt time.Time) db.ReminderOccurrence {
	e.t.Helper()
	row, err := e.store.Queries().CreateOccurrence(context.Background(), db.CreateOccurrenceParams{
		ReminderID:  parseTestUUID(e.t, reminderID),
		ScheduledAt: scheduledAt,
	})
	if err != nil {
		e.t.Fatalf("create occurrence for %s: %v", reminderID, err)
	}
	return row
}

// occurrenceStatus reads the stored status of an occurrence.
func (e *testEnv) occurrenceStatus(occurrenceID pgtype.UUID) string {
	e.t.Helper()
	var status string
	if err := e.store.Pool().QueryRow(context.Background(),
		"SELECT status FROM reminder_occurrences WHERE id = $1", occurrenceID,
	).Scan(&status); err != nil {
		e.t.Fatalf("read occurrence status: %v", err)
	}
	return status
}

// occurrenceRow is the stored state of one occurrence, read directly so an
// assertion never depends on the code under test.
type occurrenceRow struct {
	ID           pgtype.UUID
	ScheduledAt  time.Time
	FirstSentAt  pgtype.Timestamptz
	RepeatCount  int32
	NextRepeatAt pgtype.Timestamptz
	Status       string
	CompletedAt  pgtype.Timestamptz
	Deliveries   []byte
}

// occurrence reads one occurrence by id.
func (e *testEnv) occurrence(id pgtype.UUID) occurrenceRow {
	e.t.Helper()
	var row occurrenceRow
	if err := e.store.Pool().QueryRow(context.Background(), `
		SELECT id, scheduled_at, first_sent_at, repeat_count, next_repeat_at,
		       status, completed_at, deliveries
		FROM reminder_occurrences
		WHERE id = $1`, id,
	).Scan(&row.ID, &row.ScheduledAt, &row.FirstSentAt, &row.RepeatCount,
		&row.NextRepeatAt, &row.Status, &row.CompletedAt, &row.Deliveries); err != nil {
		e.t.Fatalf("read occurrence %s: %v", formatTestUUID(id), err)
	}
	return row
}

// activeOccurrence reads the active occurrence of a reminder.
func (e *testEnv) activeOccurrence(reminderID string) occurrenceRow {
	e.t.Helper()
	var row occurrenceRow
	err := e.store.Pool().QueryRow(context.Background(), `
		SELECT id, scheduled_at, first_sent_at, repeat_count, next_repeat_at,
		       status, completed_at, deliveries
		FROM reminder_occurrences
		WHERE reminder_id = $1 AND status = 'active'`,
		parseTestUUID(e.t, reminderID),
	).Scan(&row.ID, &row.ScheduledAt, &row.FirstSentAt, &row.RepeatCount,
		&row.NextRepeatAt, &row.Status, &row.CompletedAt, &row.Deliveries)
	if err != nil {
		e.t.Fatalf("read active occurrence of %s: %v", reminderID, err)
	}
	return row
}

// occurrenceCount counts every occurrence of a reminder.
func (e *testEnv) occurrenceCount(reminderID string) int {
	e.t.Helper()
	var count int
	if err := e.store.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM reminder_occurrences WHERE reminder_id = $1",
		parseTestUUID(e.t, reminderID),
	).Scan(&count); err != nil {
		e.t.Fatalf("count occurrences of %s: %v", reminderID, err)
	}
	return count
}

// reminderState is the scheduler-managed part of a reminder row.
type reminderState struct {
	NextDueAt pgtype.Timestamptz
	LastError *string
	LockUntil pgtype.Timestamptz
}

// state reads the scheduler-managed columns of a reminder.
func (e *testEnv) state(reminderID string) reminderState {
	e.t.Helper()
	var state reminderState
	if err := e.store.Pool().QueryRow(context.Background(),
		"SELECT next_due_at, last_error, lock_until FROM reminders WHERE id = $1",
		parseTestUUID(e.t, reminderID),
	).Scan(&state.NextDueAt, &state.LastError, &state.LockUntil); err != nil {
		e.t.Fatalf("read reminder %s: %v", reminderID, err)
	}
	return state
}

// fakeDelivery is a messenger that records everything the scheduler sends and
// can be told to fail.
type fakeDelivery struct {
	mu    sync.Mutex
	name  string
	sent  []sentReminder
	edits []editCall
	// events records the order of delivery calls, for example "send:1" and
	// "delete:1", so a test can prove the new message exists before the old one
	// is removed.
	events []string

	// failSends is how many upcoming Send calls fail, to simulate a messenger
	// outage.
	failSends int
	// beforeSend runs outside the lock before every Send; the stale-lease test
	// uses it to hold one send in flight.
	beforeSend func()
	nextID     int
}

// sentReminder is one recorded Send call.
type sentReminder struct {
	principalID string
	message     unibot.Message
	ref         unibot.SentMessage
}

// editCall is one recorded Edit call.
type editCall struct {
	sent    []unibot.SentMessage
	message unibot.Message
}

func newFakeDelivery() *fakeDelivery { return &fakeDelivery{name: "fake"} }

func (d *fakeDelivery) Send(_ context.Context, principalID string, message unibot.Message) ([]unibot.SentMessage, error) {
	if d.beforeSend != nil {
		d.beforeSend()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failSends > 0 {
		d.failSends--
		d.events = append(d.events, "send-failed")
		return nil, errors.New("messenger is down")
	}
	d.nextID++
	id := strconv.Itoa(d.nextID)
	ref := unibot.SentMessage{
		Platform:        unibot.PlatformTelegram,
		RemoteUserID:    "42",
		ConversationID:  "42",
		RemoteMessageID: id,
	}
	d.sent = append(d.sent, sentReminder{principalID: principalID, message: message, ref: ref})
	d.events = append(d.events, "send:"+id)
	return []unibot.SentMessage{ref}, nil
}

func (d *fakeDelivery) Edit(_ context.Context, sent []unibot.SentMessage, message unibot.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.edits = append(d.edits, editCall{sent: sent, message: message})
	d.events = append(d.events, "edit:"+sent[0].RemoteMessageID)
	return nil
}

func (d *fakeDelivery) Delete(_ context.Context, sent []unibot.SentMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, reference := range sent {
		d.events = append(d.events, "delete:"+reference.RemoteMessageID)
	}
	return nil
}

// sentCount returns how many messages were delivered.
func (d *fakeDelivery) sentCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sent)
}

// messages returns the delivered messages in order.
func (d *fakeDelivery) messages() []unibot.Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]unibot.Message, 0, len(d.sent))
	for _, item := range d.sent {
		out = append(out, item.message)
	}
	return out
}

// lastMessage returns the newest delivered message.
func (d *fakeDelivery) lastMessage() unibot.Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.sent) == 0 {
		return unibot.Message{}
	}
	return d.sent[len(d.sent)-1].message
}

// principals returns the principal of every delivery.
func (d *fakeDelivery) principals() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.sent))
	for _, item := range d.sent {
		out = append(out, item.principalID)
	}
	return out
}

// lastEdit returns the newest Edit call.
func (d *fakeDelivery) lastEdit() (editCall, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.edits) == 0 {
		return editCall{}, false
	}
	return d.edits[len(d.edits)-1], true
}

// eventLog returns the recorded delivery calls in order.
func (d *fakeDelivery) eventLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.events...)
}

// failNextSends makes the next count deliveries fail.
func (d *fakeDelivery) failNextSends(count int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failSends = count
}

// formatTestUUID renders a uuid column in canonical form.
func formatTestUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmtUUID(b)
}

func fmtUUID(b [16]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, value := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[value>>4], hex[value&0xf])
	}
	return string(out)
}

func parseTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var parsed pgtype.UUID
	if err := parsed.Scan(value); err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}
