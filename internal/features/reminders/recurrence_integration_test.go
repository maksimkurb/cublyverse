package reminders_test

import (
	"context"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
)

// recurrenceInput is a rule with no days of its own, so a test overrides only
// the parts the case is about.
func recurrenceInput() reminders.Input {
	return reminders.Input{
		Message:       "Напоминание",
		Timezone:      "UTC",
		Kind:          reminders.Daily,
		TimeHHMM:      "09:00",
		RepeatMinutes: 10,
		Enabled:       true,
	}
}

// TestServiceRecurrenceKinds covers the new shapes end to end: the stored row,
// the computed next due time and the canonical fields of the model.
func TestServiceRecurrenceKinds(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	tests := []struct {
		name  string
		input reminders.Input
		next  time.Time
	}{
		{
			name: "once",
			input: func() reminders.Input {
				in := recurrenceInput()
				in.Kind = reminders.Once
				in.StartDate = "2026-01-10"
				in.TimeHHMM = "10:00"
				in.Timezone = "Europe/Moscow"
				return in
			}(),
			next: time.Date(2026, 1, 10, 7, 0, 0, 0, time.UTC),
		},
		{
			name: "daily every third day",
			input: func() reminders.Input {
				in := recurrenceInput()
				in.Kind = reminders.Daily
				in.Interval = 3
				in.StartDate = "2026-01-06"
				return in
			}(),
			next: time.Date(2026, 1, 6, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "weekly every second week",
			input: func() reminders.Input {
				in := recurrenceInput()
				in.Kind = reminders.Weekly
				in.Interval = 2
				in.Days = []int{1, 4}
				in.StartDate = "2026-01-05"
				in.TimeHHMM = "10:00"
				return in
			}(),
			next: time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "monthly second tuesday",
			input: func() reminders.Input {
				in := recurrenceInput()
				in.Kind = reminders.Monthly
				in.MonthWeekPosition = 2
				in.MonthWeekday = 2
				in.StartDate = "2026-01-01"
				return in
			}(),
			next: time.Date(2026, 1, 13, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "yearly",
			input: func() reminders.Input {
				in := recurrenceInput()
				in.Kind = reminders.Yearly
				in.StartDate = "2026-03-20"
				in.TimeHHMM = "10:00"
				return in
			}(),
			next: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := env.service.Create(ctx, principal, tt.input)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if created.NextDueAt == nil || !created.NextDueAt.Equal(tt.next) {
				t.Fatalf("next_due_at = %v, want %s", created.NextDueAt, tt.next.Format(time.RFC3339))
			}
			if created.Interval != max(tt.input.Interval, 1) {
				t.Errorf("interval = %d, want %d", created.Interval, max(tt.input.Interval, 1))
			}
			if created.StartDate.String() != tt.input.StartDate {
				t.Errorf("start_date = %s, want %s", created.StartDate, tt.input.StartDate)
			}
			if created.Finished() {
				t.Error("a new reminder is already finished")
			}
			if created.OccurrencesFired != 0 {
				t.Errorf("occurrences_fired = %d, want 0", created.OccurrencesFired)
			}
		})
	}

	// A one-time reminder in the past is rejected instead of silently storing a
	// rule that can never fire.
	past := recurrenceInput()
	past.Kind = reminders.Once
	past.StartDate = "2020-01-01"
	past.TimeHHMM = "10:00"
	past.Timezone = "Europe/Moscow"
	if _, err := env.service.Create(ctx, principal, past); err == nil {
		t.Fatal("Create of a past one-time reminder returned no error")
	}
}

// TestSchedulerFinishesOnceReminder proves the one-time flow: it fires once, the
// next due time disappears, and the confirmation marks it finished. A later tick
// has nothing to do.
func TestSchedulerFinishesOnceReminder(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	input := recurrenceInput()
	input.Kind = reminders.Once
	input.StartDate = "2026-01-05"
	input.TimeHHMM = "12:00" // 09:00 UTC, after the fixed clock
	input.Timezone = "Europe/Moscow"
	reminder := env.dueReminder(principal, input)

	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent = %d, want 1", got)
	}
	state := env.state(reminder.ID)
	if state.NextDueAt.Valid {
		t.Errorf("next_due_at = %v, want NULL after the only occurrence", state.NextDueAt)
	}

	completed, err := env.service.Complete(ctx, principal, reminder.ID)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !completed.Finished() {
		t.Error("a confirmed one-time reminder is not finished")
	}
	if completed.Enabled {
		t.Error("a finished reminder is still enabled")
	}
	if completed.NextDueAt != nil {
		t.Errorf("next_due_at = %v, want nil", completed.NextDueAt)
	}
	if completed.OccurrencesFired != 1 {
		t.Errorf("occurrences_fired = %d, want 1", completed.OccurrencesFired)
	}

	env.advance(24 * time.Hour)
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("Tick after finishing: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent after finishing = %d, want still 1", got)
	}
}

// TestSchedulerEndCountFinishes covers "after N occurrences": the second firing
// has no next due time, and its confirmation marks the reminder finished.
func TestSchedulerEndCountFinishes(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	input := recurrenceInput()
	input.Kind = reminders.Daily
	input.StartDate = "2026-01-05"
	input.TimeHHMM = "12:00"
	input.Timezone = "Europe/Moscow"
	input.EndKind = reminders.EndCount
	input.EndCount = 2
	reminder := env.dueReminder(principal, input)

	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	first := env.state(reminder.ID)
	if !first.NextDueAt.Valid {
		t.Fatal("the first firing did not schedule the second occurrence")
	}
	if _, err := env.service.Complete(ctx, principal, reminder.ID); err != nil {
		t.Fatalf("first Complete: %v", err)
	}

	// Move to the second (last) occurrence.
	env.now = first.NextDueAt.Time
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	second := env.state(reminder.ID)
	if second.NextDueAt.Valid {
		t.Errorf("next_due_at = %v, want NULL after the last occurrence", second.NextDueAt)
	}
	completed, err := env.service.Complete(ctx, principal, reminder.ID)
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if !completed.Finished() {
		t.Error("the last occurrence of a count rule did not finish the reminder")
	}
	if completed.OccurrencesFired != 2 {
		t.Errorf("occurrences_fired = %d, want 2", completed.OccurrencesFired)
	}
}

// TestUpdateResetsFiredCounter proves that editing the rule starts a new cycle:
// a finished reminder becomes a normal scheduled one again.
func TestUpdateResetsFiredCounter(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	input := recurrenceInput()
	input.Kind = reminders.Once
	input.StartDate = "2026-01-05"
	input.TimeHHMM = "12:00"
	input.Timezone = "Europe/Moscow"
	reminder := env.dueReminder(principal, input)
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if _, err := env.service.Complete(ctx, principal, reminder.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	updated, err := env.service.Update(ctx, principal, reminder.ID, reminders.Input{
		Message:       "Новое правило",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Daily,
		TimeHHMM:      "12:00",
		RepeatMinutes: 10,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Finished() {
		t.Error("an edited reminder is still finished")
	}
	if !updated.Enabled {
		t.Error("an edited reminder is disabled")
	}
	if updated.OccurrencesFired != 0 {
		t.Errorf("occurrences_fired = %d, want 0 after editing", updated.OccurrencesFired)
	}
	if updated.NextDueAt == nil {
		t.Error("an edited repeating reminder has no next_due_at")
	}
}
