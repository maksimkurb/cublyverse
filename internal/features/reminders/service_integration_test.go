package reminders_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// validInput is a rule that is valid in every respect: Monday, Wednesday and
// Friday at 12:00 Moscow time, which is still ahead of the fixed test clock.
func validInput() reminders.Input {
	return reminders.Input{
		Message:       "Выпить таблетку",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Weekly,
		Days:          []int{1, 3, 5},
		TimeHHMM:      "12:00",
		RepeatMinutes: 15,
		Enabled:       true,
	}
}

func TestServiceLifecycle(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	created, err := env.service.Create(ctx, principal, validInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create returned no id")
	}
	if created.Message != "Выпить таблетку" {
		t.Errorf("message = %q", created.Message)
	}
	if created.Kind != reminders.Weekly {
		t.Errorf("kind = %q, want weekly", created.Kind)
	}
	if got := created.Days; len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Errorf("days = %v, want [1 3 5]", got)
	}
	if created.RepeatMinutes != 15 || !created.Enabled {
		t.Errorf("repeat = %d, enabled = %v", created.RepeatMinutes, created.Enabled)
	}
	// 12:00 Moscow on Monday 2026-01-05 is 09:00 UTC.
	if created.NextDueAt == nil || !created.NextDueAt.Equal(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_due_at = %v, want 2026-01-05T09:00:00Z", created.NextDueAt)
	}
	if created.ActiveOccurrence != nil {
		t.Error("a new reminder has an active occurrence")
	}

	list, err := env.service.List(ctx, principal)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("List = %+v, want the created reminder", list)
	}

	got, err := env.service.Get(ctx, principal, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.Message != created.Message {
		t.Errorf("Get = %+v, want the created reminder", got)
	}

	// PUT replaces the whole rule: monthly on the 15th at 10:00 Moscow time.
	updated, err := env.service.Update(ctx, principal, created.ID, reminders.Input{
		Message:       "  Новое напоминание  ",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Monthly,
		Days:          []int{15, 15},
		TimeHHMM:      "10:00",
		RepeatMinutes: 30,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Message != "Новое напоминание" {
		t.Errorf("message = %q, want the trimmed one", updated.Message)
	}
	if updated.Kind != reminders.Monthly {
		t.Errorf("kind = %q, want monthly", updated.Kind)
	}
	if len(updated.Days) != 1 || updated.Days[0] != 15 {
		t.Errorf("days = %v, want [15]", updated.Days)
	}
	if updated.RepeatMinutes != 30 {
		t.Errorf("repeat = %d, want 30", updated.RepeatMinutes)
	}
	// 10:00 Moscow on the 15th is 07:00 UTC.
	if updated.NextDueAt == nil || !updated.NextDueAt.Equal(time.Date(2026, 1, 15, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_due_at = %v, want 2026-01-15T07:00:00Z", updated.NextDueAt)
	}

	if err := env.service.Delete(ctx, principal, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := env.service.Get(ctx, principal, created.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	list, err = env.service.List(ctx, principal)
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after Delete = %+v, want empty", list)
	}
}

func TestServiceValidation(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	tests := []struct {
		name  string
		apply func(*reminders.Input)
	}{
		{name: "empty message", apply: func(in *reminders.Input) { in.Message = "" }},
		{name: "whitespace message", apply: func(in *reminders.Input) { in.Message = "   " }},
		{name: "message too long", apply: func(in *reminders.Input) { in.Message = strings.Repeat("я", 1501) }},
		{name: "repeat zero", apply: func(in *reminders.Input) { in.RepeatMinutes = 0 }},
		{name: "repeat too long", apply: func(in *reminders.Input) { in.RepeatMinutes = 1441 }},
		{name: "no days", apply: func(in *reminders.Input) { in.Days = nil }},
		{name: "weekly day eight", apply: func(in *reminders.Input) { in.Days = []int{8} }},
		{name: "weekly day zero", apply: func(in *reminders.Input) { in.Days = []int{0} }},
		{name: "monthly day thirty-two", apply: func(in *reminders.Input) {
			in.Kind = reminders.Monthly
			in.Days = []int{32}
		}},
		{name: "unpadded time", apply: func(in *reminders.Input) { in.TimeHHMM = "9:00" }},
		{name: "hour out of range", apply: func(in *reminders.Input) { in.TimeHHMM = "24:00" }},
		{name: "unknown time zone", apply: func(in *reminders.Input) { in.Timezone = "Mars/Olympus" }},
		{name: "local time zone", apply: func(in *reminders.Input) { in.Timezone = "Local" }},
		{name: "unknown kind", apply: func(in *reminders.Input) { in.Kind = reminders.Kind("fortnightly") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validInput()
			tt.apply(&input)
			created, err := env.service.Create(ctx, principal, input)
			var invalid *reminders.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Create = (%+v, %v), want a ValidationError", created, err)
			}
			if invalid.Message == "" {
				t.Error("ValidationError has no message")
			}
		})
	}

	// The boundary values are accepted: 1 and 1500 runes, 1 and 1440 minutes.
	for _, input := range []reminders.Input{
		validInput(),
		func() reminders.Input {
			in := validInput()
			in.Message = strings.Repeat("я", 1500)
			in.RepeatMinutes = 1
			return in
		}(),
		func() reminders.Input {
			in := validInput()
			in.RepeatMinutes = 1440
			in.Days = []int{7}
			return in
		}(),
	} {
		if _, err := env.service.Create(ctx, principal, input); err != nil {
			t.Fatalf("Create with a boundary value: %v", err)
		}
	}

	// The database repeats the bounds, so a bug or a manual insert cannot store
	// a rule the service would reject.
	t.Run("database constraints", func(t *testing.T) {
		userID := parseTestUUID(t, principal)
		type row struct {
			message string
			kind    string
			days    []int16
			hhmm    string
			repeat  int32
		}
		valid := row{message: "x", kind: "weekly", days: []int16{1}, hhmm: "09:00", repeat: 10}
		insert := func(r row) error {
			_, err := env.store.Pool().Exec(ctx, `
				INSERT INTO reminders (
					user_id, message, timezone, schedule_kind, schedule_days,
					time_hhmm, repeat_minutes, start_date
				)
				VALUES ($1, $2, 'UTC', $3, $4, $5, $6, DATE '2026-01-05')`,
				userID, r.message, r.kind, r.days, r.hhmm, r.repeat)
			return err
		}
		if err := insert(valid); err != nil {
			t.Fatalf("the database rejected a valid row: %v", err)
		}
		cases := []struct {
			name   string
			mutate func(*row)
		}{
			{name: "empty message", mutate: func(r *row) { r.message = "" }},
			{name: "message too long", mutate: func(r *row) { r.message = strings.Repeat("я", 1501) }},
			{name: "repeat zero", mutate: func(r *row) { r.repeat = 0 }},
			{name: "repeat too long", mutate: func(r *row) { r.repeat = 1441 }},
			{name: "unpadded time", mutate: func(r *row) { r.hhmm = "9:00" }},
			{name: "no days", mutate: func(r *row) { r.days = []int16{} }},
			{name: "weekly day eight", mutate: func(r *row) { r.days = []int16{8} }},
			{name: "monthly day thirty-two", mutate: func(r *row) {
				r.kind = "monthly"
				r.days = []int16{32}
			}},
			{name: "unknown kind", mutate: func(r *row) { r.kind = "fortnightly" }},
		}
		for _, c := range cases {
			candidate := valid
			c.mutate(&candidate)
			if err := insert(candidate); err == nil {
				t.Errorf("%s: the database accepted the row", c.name)
			}
		}

		// At most one active occurrence per reminder, enforced by the partial
		// unique index rather than by the service.
		created, err := env.service.Create(ctx, principal, validInput())
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		env.createOccurrence(created.ID, *created.NextDueAt)
		if _, err := env.store.Queries().CreateOccurrence(ctx, db.CreateOccurrenceParams{
			ReminderID:  parseTestUUID(t, created.ID),
			ScheduledAt: *created.NextDueAt,
		}); err == nil {
			t.Fatal("a second active occurrence was accepted")
		}
	})
}

func TestServiceOwnershipIsolation(t *testing.T) {
	env := newTestEnv(t)
	alice := env.createUser("alice")
	bob := env.createUser("bob")
	ctx := context.Background()

	created, err := env.service.Create(ctx, alice, validInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	occurrence := env.createOccurrence(created.ID, *created.NextDueAt)

	// Bob sees nothing of Alice.
	list, err := env.service.List(ctx, bob)
	if err != nil {
		t.Fatalf("Bob List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("Bob sees %d reminders of Alice", len(list))
	}
	if _, err := env.service.Get(ctx, bob, created.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("Bob Get = %v, want ErrNotFound", err)
	}
	if _, err := env.service.Update(ctx, bob, created.ID, validInput()); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("Bob Update = %v, want ErrNotFound", err)
	}
	if _, err := env.service.Complete(ctx, bob, created.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("Bob Complete = %v, want ErrNotFound", err)
	}
	if err := env.service.Delete(ctx, bob, created.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("Bob Delete = %v, want ErrNotFound", err)
	}
	// The failed attempts changed nothing: the occurrence is still active and
	// Alice still sees her reminder.
	if status := env.occurrenceStatus(occurrence.ID); status != "active" {
		t.Fatalf("occurrence status = %q after foreign attempts, want active", status)
	}
	if _, err := env.service.Get(ctx, alice, created.ID); err != nil {
		t.Fatalf("Alice Get after foreign attempts: %v", err)
	}

	// A malformed id is indistinguishable from a foreign one.
	for _, id := range []string{"not-a-uuid", "", "00000000-0000-0000-0000-000000000000"} {
		if _, err := env.service.Get(ctx, alice, id); !errors.Is(err, reminders.ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", id, err)
		}
		if err := env.service.Delete(ctx, alice, id); !errors.Is(err, reminders.ErrNotFound) {
			t.Errorf("Delete(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

func TestServiceUpdateCancelsActiveOccurrence(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	created, err := env.service.Create(ctx, principal, validInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	occurrence := env.createOccurrence(created.ID, *created.NextDueAt)

	got, err := env.service.Get(ctx, principal, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ActiveOccurrence == nil {
		t.Fatal("Get did not report the active occurrence")
	}
	if got.ActiveOccurrence.ID != formatTestUUID(occurrence.ID) {
		t.Errorf("active occurrence = %s, want %s", got.ActiveOccurrence.ID, formatTestUUID(occurrence.ID))
	}

	// Editing the rule drops the pending firing.
	updated, err := env.service.Update(ctx, principal, created.ID, reminders.Input{
		Message:       "Другое напоминание",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Weekly,
		Days:          []int{2},
		TimeHHMM:      "18:00",
		RepeatMinutes: 5,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ActiveOccurrence != nil {
		t.Error("Update returned an active occurrence")
	}
	if status := env.occurrenceStatus(occurrence.ID); status != "cancelled" {
		t.Fatalf("occurrence status = %q, want cancelled", status)
	}
	// 18:00 Moscow on Tuesday 2026-01-06 is 15:00 UTC.
	if updated.NextDueAt == nil || !updated.NextDueAt.Equal(time.Date(2026, 1, 6, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_due_at = %v, want 2026-01-06T15:00:00Z", updated.NextDueAt)
	}

	// Disabling clears the next due time and cancels a pending occurrence.
	env.createOccurrence(created.ID, *updated.NextDueAt)
	disabled, err := env.service.Update(ctx, principal, created.ID, reminders.Input{
		Message:       "Другое напоминание",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Weekly,
		Days:          []int{2},
		TimeHHMM:      "18:00",
		RepeatMinutes: 5,
		Enabled:       false,
	})
	if err != nil {
		t.Fatalf("Update (disabled): %v", err)
	}
	if disabled.Enabled {
		t.Error("reminder is still enabled")
	}
	if disabled.NextDueAt != nil {
		t.Errorf("next_due_at = %v, want nil for a disabled reminder", disabled.NextDueAt)
	}

	// Enabling computes the nearest future occurrence.
	enabled, err := env.service.Update(ctx, principal, created.ID, reminders.Input{
		Message:       "Другое напоминание",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Weekly,
		Days:          []int{2},
		TimeHHMM:      "18:00",
		RepeatMinutes: 5,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("Update (enabled): %v", err)
	}
	if enabled.NextDueAt == nil || !enabled.NextDueAt.After(env.now) {
		t.Fatalf("next_due_at = %v, want a future instant", enabled.NextDueAt)
	}

	// A disabled reminder is stored without a next due time.
	draft, err := env.service.Create(ctx, principal, reminders.Input{
		Message:       "Черновик",
		Timezone:      "UTC",
		Kind:          reminders.Weekly,
		Days:          []int{1},
		TimeHHMM:      "09:00",
		RepeatMinutes: 10,
		Enabled:       false,
	})
	if err != nil {
		t.Fatalf("Create (disabled): %v", err)
	}
	if draft.Enabled {
		t.Error("the draft reminder is enabled")
	}
	if draft.NextDueAt != nil {
		t.Errorf("next_due_at = %v, want nil for a disabled reminder", draft.NextDueAt)
	}
}

func TestServiceComplete(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	created, err := env.service.Create(ctx, principal, validInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := env.service.Complete(ctx, principal, created.ID); !errors.Is(err, reminders.ErrNoActiveOccurrence) {
		t.Fatalf("Complete without an occurrence = %v, want ErrNoActiveOccurrence", err)
	}

	occurrence := env.createOccurrence(created.ID, *created.NextDueAt)
	env.advance(2 * time.Minute)
	completed, err := env.service.Complete(ctx, principal, created.ID)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.ActiveOccurrence != nil {
		t.Error("Complete returned an active occurrence")
	}
	if status := env.occurrenceStatus(occurrence.ID); status != "completed" {
		t.Fatalf("occurrence status = %q, want completed", status)
	}
	var completedAt pgtype.Timestamptz
	if err := env.store.Pool().QueryRow(ctx,
		"SELECT completed_at FROM reminder_occurrences WHERE id = $1", occurrence.ID,
	).Scan(&completedAt); err != nil {
		t.Fatalf("read completed_at: %v", err)
	}
	if !completedAt.Valid || !completedAt.Time.Equal(env.now) {
		t.Fatalf("completed_at = %v, want %v", completedAt, env.now)
	}

	if _, err := env.service.Complete(ctx, principal, created.ID); !errors.Is(err, reminders.ErrNoActiveOccurrence) {
		t.Fatalf("second Complete = %v, want ErrNoActiveOccurrence", err)
	}
}
