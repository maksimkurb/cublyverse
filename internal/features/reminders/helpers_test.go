package reminders_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

// fixedNow is the clock of the integration tests: Monday 2026-01-05 08:00 UTC.
// In Europe/Moscow (UTC+3) it is 11:00 on Monday, so a 09:00 reminder has
// already passed today and a 12:00 one is still ahead.
var fixedNow = time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)

// testEnv is a real service stack: a PostgreSQL container, the migrations and
// the reminders service with a controllable clock.
type testEnv struct {
	t       *testing.T
	store   *store.Store
	service *reminders.Service
	now     time.Time
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

	env := &testEnv{t: t, store: st, now: fixedNow}
	env.service = reminders.New(st,
		reminders.WithLogger(logger),
		reminders.WithClock(func() time.Time { return env.now }),
	)
	return env
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
