package reminders_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/api"
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// TestSchedulerFirstFire is the basic firing: one message, the button, the
// occurrence and a next due time computed from now.
func TestSchedulerFirstFire(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages = %d, want 1", got)
	}
	if principals := env.delivery.principals(); len(principals) != 1 || principals[0] != principal {
		t.Fatalf("delivered to %v, want [%s]", principals, principal)
	}

	message := env.delivery.lastMessage()
	wantHTML := "Выпить таблетку\n\nПервое напоминание: <b>05.01.2026 12:00</b>"
	if message.HTML != wantHTML {
		t.Errorf("html = %q, want %q", message.HTML, wantHTML)
	}
	wantText := "Выпить таблетку\n\nПервое напоминание: 05.01.2026 12:00"
	if message.Text != wantText {
		t.Errorf("text = %q, want %q", message.Text, wantText)
	}
	if strings.Contains(message.HTML, "Количество повторов") {
		t.Error("the first message has a repeat counter")
	}

	occurrence := env.activeOccurrence(reminder.ID)
	action := message.Actions[0][0]
	if action.ID != reminders.ActionDone {
		t.Errorf("action id = %q, want %q", action.ID, reminders.ActionDone)
	}
	wantPayload := reminder.ID + ":" + formatTestUUID(occurrence.ID)
	if action.Payload != wantPayload {
		t.Errorf("action payload = %q, want %q", action.Payload, wantPayload)
	}
	if string(action.PrincipalID) != principal {
		t.Errorf("action principal = %q, want the owner %q", action.PrincipalID, principal)
	}
	if action.Kind != unibot.ActionSubmit {
		t.Errorf("action kind = %q, want submit", action.Kind)
	}
	if action.TTL < 48*time.Hour {
		t.Errorf("action ttl = %v, want at least 48h", action.TTL)
	}

	if occurrence.Status != "active" {
		t.Errorf("occurrence status = %q, want active", occurrence.Status)
	}
	if !occurrence.ScheduledAt.Equal(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("scheduled_at = %v, want 2026-01-05T09:00:00Z", occurrence.ScheduledAt)
	}
	if !occurrence.FirstSentAt.Valid || !occurrence.FirstSentAt.Time.Equal(env.now) {
		t.Errorf("first_sent_at = %v, want %v", occurrence.FirstSentAt, env.now)
	}
	if occurrence.RepeatCount != 1 {
		t.Errorf("repeat_count = %d, want 1 (the first message)", occurrence.RepeatCount)
	}
	// Fifteen minutes later, in the reminder's own terms.
	if !occurrence.NextRepeatAt.Valid || !occurrence.NextRepeatAt.Time.Equal(env.now.Add(15*time.Minute)) {
		t.Errorf("next_repeat_at = %v, want %v", occurrence.NextRepeatAt, env.now.Add(15*time.Minute))
	}

	// The delivery refs are stored, so a repeat can delete this message and a
	// completion can edit it.
	var deliveries []map[string]string
	if err := json.Unmarshal(occurrence.Deliveries, &deliveries); err != nil {
		t.Fatalf("decode deliveries %q: %v", occurrence.Deliveries, err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %v, want one entry", deliveries)
	}
	if deliveries[0]["platform"] != "telegram" || deliveries[0]["conversation_id"] != "42" || deliveries[0]["message_id"] != "1" {
		t.Errorf("delivery record = %v", deliveries[0])
	}

	state := env.state(reminder.ID)
	// Monday 12:00 Moscow has passed; the next firing is Wednesday.
	if !state.NextDueAt.Valid || !state.NextDueAt.Time.Equal(time.Date(2026, 1, 7, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("next_due_at = %v, want 2026-01-07T09:00:00Z", state.NextDueAt)
	}
	if state.LastError != nil {
		t.Errorf("last_error = %q, want none", *state.LastError)
	}
	if state.LockUntil.Valid {
		t.Errorf("lock_until = %v, want the lease released", state.LockUntil)
	}

	// A second tick immediately after the fire sends nothing: the next due time
	// is in the future.
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages after the second tick = %d, want 1", got)
	}
}

// TestSchedulerRepeatSequence covers the repeat cycle: a new message with the
// counter, then the previous message disappears.
func TestSchedulerRepeatSequence(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	scheduler := env.scheduler()
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("first Tick: %v", err)
	}

	// Not due yet: repeats follow repeat_minutes, not the tick interval.
	env.advance(14 * time.Minute)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick before the repeat: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages before the repeat = %d, want 1", got)
	}

	env.advance(time.Minute)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("repeat Tick: %v", err)
	}
	if got := env.delivery.sentCount(); got != 2 {
		t.Fatalf("sent messages after the first repeat = %d, want 2", got)
	}
	first := env.delivery.lastMessage()
	if !strings.Contains(first.HTML, "Первое напоминание: <b>05.01.2026 12:00</b>") {
		t.Errorf("repeat html = %q, want the original text", first.HTML)
	}
	if !strings.Contains(first.HTML, "Количество повторов: <b>1</b>") {
		t.Errorf("repeat html = %q, want the counter 1", first.HTML)
	}

	occurrence := env.activeOccurrence(reminder.ID)
	if occurrence.RepeatCount != 2 {
		t.Errorf("repeat_count = %d, want 2", occurrence.RepeatCount)
	}
	if !occurrence.NextRepeatAt.Valid || !occurrence.NextRepeatAt.Time.Equal(env.now.Add(15*time.Minute)) {
		t.Errorf("next_repeat_at = %v, want %v", occurrence.NextRepeatAt, env.now.Add(15*time.Minute))
	}
	// The new message must exist before the old one is deleted.
	wantEvents := []string{"send:1", "send:2", "delete:1"}
	if got := env.delivery.eventLog(); !equalStrings(got, wantEvents) {
		t.Errorf("delivery log = %v, want %v", got, wantEvents)
	}

	env.advance(15 * time.Minute)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("second repeat Tick: %v", err)
	}
	if got := env.delivery.lastMessage().HTML; !strings.Contains(got, "Количество повторов: <b>2</b>") {
		t.Errorf("second repeat html = %q, want the counter 2", got)
	}
	if got := env.delivery.eventLog(); !equalStrings(got, []string{"send:1", "send:2", "delete:1", "send:3", "delete:2"}) {
		t.Errorf("delivery log = %v", got)
	}
	if state := env.state(reminder.ID); state.LockUntil.Valid {
		t.Errorf("lock_until = %v after a repeat, want it released", state.LockUntil)
	}
}

// TestSchedulerCatchUpAfterOutage proves a long stop of the process produces one
// catch-up message, not one per missed schedule.
func TestSchedulerCatchUpAfterOutage(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	// Three days down: two firings were missed (Monday, Wednesday).
	env.advance(72 * time.Hour)

	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages after the outage = %d, want 1", got)
	}
	if got := env.occurrenceCount(reminder.ID); got != 1 {
		t.Fatalf("occurrences after the outage = %d, want 1", got)
	}
	state := env.state(reminder.ID)
	// Thursday 09:00Z: the next firing is Friday 09:00Z, computed from now and
	// not from the missed Monday.
	if !state.NextDueAt.Valid || !state.NextDueAt.Time.Equal(time.Date(2026, 1, 9, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_due_at = %v, want 2026-01-09T09:00:00Z", state.NextDueAt)
	}
	if !state.NextDueAt.Time.After(env.now) {
		t.Fatalf("next_due_at %v is not after now %v", state.NextDueAt.Time, env.now)
	}
}

// TestSchedulerDeliveryFailureIsRetried covers a messenger outage: the error is
// recorded, nothing else changes and the next tick tries again.
func TestSchedulerDeliveryFailureIsRetried(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	scheduler := env.scheduler()

	env.delivery.failNextSends(1)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick with a failing messenger: %v", err)
	}
	if got := env.delivery.sentCount(); got != 0 {
		t.Fatalf("sent messages = %d, want 0", got)
	}
	if got := env.occurrenceCount(reminder.ID); got != 0 {
		t.Fatalf("occurrences after a failed send = %d, want 0", got)
	}
	state := env.state(reminder.ID)
	if state.LastError == nil || !strings.Contains(*state.LastError, "messenger is down") {
		t.Fatalf("last_error = %v, want the messenger error", state.LastError)
	}
	if !state.NextDueAt.Valid || !state.NextDueAt.Time.Equal(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_due_at = %v, want the unchanged due time", state.NextDueAt)
	}
	if state.LockUntil.Valid {
		t.Fatalf("lock_until = %v after a failed send, want it released", state.LockUntil)
	}

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("retry Tick: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages after the retry = %d, want 1", got)
	}
	if got := env.occurrenceCount(reminder.ID); got != 1 {
		t.Fatalf("occurrences after the retry = %d, want 1", got)
	}
	if state := env.state(reminder.ID); state.LastError != nil {
		t.Fatalf("last_error = %q after a successful retry, want it cleared", *state.LastError)
	}
}

// TestSchedulerTwoWorkersDoNotDoubleSend runs two schedulers on the same
// database: SKIP LOCKED and the lease make exactly one of them fire.
func TestSchedulerTwoWorkersDoNotDoubleSend(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	first := env.scheduler()
	second := reminders.NewScheduler(env.service, reminders.SchedulerConfig{})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, scheduler := range []*reminders.Scheduler{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = scheduler.Tick(ctx)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages by two workers = %d, want 1", got)
	}
	if got := env.occurrenceCount(reminder.ID); got != 1 {
		t.Fatalf("occurrences by two workers = %d, want 1", got)
	}
}

// TestSchedulerStaleLeaseCannotOverwriteState holds one send in flight until its
// lease expires and lets a second worker take over. The first worker must not
// overwrite the newer state.
func TestSchedulerStaleLeaseCannotOverwriteState(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())

	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	env.delivery.beforeSend = func() {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}

	stale := env.scheduler()
	staleResult := make(chan error, 1)
	go func() { staleResult <- stale.Tick(ctx) }()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first worker did not start sending")
	}

	// The lease lasts 55 seconds; a minute later another worker may take over.
	env.advance(time.Minute)
	takeover := env.scheduler()
	if err := takeover.Tick(ctx); err != nil {
		t.Fatalf("second worker Tick: %v", err)
	}
	takeoverNow := env.now

	close(release)
	select {
	case err := <-staleResult:
		if !errors.Is(err, reminders.ErrLeaseLost) {
			t.Fatalf("stale worker returned %v, want ErrLeaseLost", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stale worker did not finish")
	}

	if got := env.occurrenceCount(reminder.ID); got != 1 {
		t.Fatalf("occurrences = %d, want 1", got)
	}
	occurrence := env.activeOccurrence(reminder.ID)
	if !occurrence.FirstSentAt.Valid || !occurrence.FirstSentAt.Time.Equal(takeoverNow) {
		t.Fatalf("first_sent_at = %v, want the takeover time %v", occurrence.FirstSentAt, takeoverNow)
	}
	if got := env.state(reminder.ID); got.LockUntil.Valid {
		t.Fatalf("lock_until = %v, want the lease released", got.LockUntil)
	}
	// Both messages were sent: the stale one cannot be unsent, but only the
	// takeover shapes the state.
	if got := env.delivery.sentCount(); got != 2 {
		t.Fatalf("sent messages = %d, want 2 (one stale, one takeover)", got)
	}
}

// TestSchedulerUpdateCancelsPendingRepeat covers a rule change while an
// occurrence waits for confirmation: the occurrence is cancelled and the pending
// repeat never fires.
func TestSchedulerUpdateCancelsPendingRepeat(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	scheduler := env.scheduler()
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	occurrence := env.activeOccurrence(reminder.ID)

	if _, err := env.service.Update(ctx, principal, reminder.ID, reminders.Input{
		Message:       "Другое напоминание",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Weekly,
		Days:          []int{2},
		TimeHHMM:      "18:00",
		RepeatMinutes: 15,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	env.advance(15 * time.Minute)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick after the update: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages = %d, want 1 (no repeat after the edit)", got)
	}
	if got := env.occurrenceStatus(occurrence.ID); got != "cancelled" {
		t.Fatalf("occurrence status = %q, want cancelled", got)
	}
}

// TestSchedulerDisableStopsEverything covers turning the rule off while an
// occurrence is active.
func TestSchedulerDisableStopsEverything(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	scheduler := env.scheduler()
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if _, err := env.service.Update(ctx, principal, reminder.ID, reminders.Input{
		Message:       "Выпить таблетку",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Weekly,
		Days:          []int{1, 3, 5},
		TimeHHMM:      "12:00",
		RepeatMinutes: 15,
		Enabled:       false,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	env.advance(48 * time.Hour)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick after disabling: %v", err)
	}
	if got := env.delivery.sentCount(); got != 1 {
		t.Fatalf("sent messages = %d, want 1 (a disabled reminder never fires)", got)
	}
	if state := env.state(reminder.ID); state.NextDueAt.Valid {
		t.Fatalf("next_due_at = %v, want none", state.NextDueAt)
	}
}

// TestCompletionViaHTTPUpdatesChatMessage is the cross-entry-point case: the
// occurrence is confirmed in the WebApp and the chat message becomes the
// completion text with the button removed.
func TestCompletionViaHTTPUpdatesChatMessage(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	ctx := context.Background()

	reminder := env.dueReminder(principal, validInput())
	scheduler := env.scheduler()
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// One repeat, so the completion line shows the counter too.
	env.advance(15 * time.Minute)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("repeat Tick: %v", err)
	}

	server := httptest.NewServer(httpapi.New(httpapi.Deps{
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Spec:      api.OpenAPISpec,
		Auth:      fakeAuth{tokens: map[string]string{"token-alice": principal}},
		Reminders: env.service,
	}))
	t.Cleanup(server.Close)

	status, body := apiRequest(t, http.MethodPost, server.URL+"/api/v1/reminders/"+reminder.ID+"/complete", "token-alice", "")
	requireStatus(t, status, http.StatusOK, body)

	edit, ok := env.delivery.lastEdit()
	if !ok {
		t.Fatal("the chat message was not edited")
	}
	if len(edit.sent) != 1 || edit.sent[0].RemoteMessageID != "2" {
		t.Errorf("edited message = %+v, want the newest one", edit.sent)
	}
	wantParts := []string{
		"✅ Выполнено: <b>05.01.2026 12:15:00</b>",
		"Первое напоминание: <b>05.01.2026 12:00</b>",
		"Количество повторов: <b>1</b>",
	}
	for _, part := range wantParts {
		if !strings.Contains(edit.message.HTML, part) {
			t.Errorf("edited html %q does not contain %q", edit.message.HTML, part)
		}
	}
	if len(edit.message.Actions) != 0 {
		t.Errorf("edited message still has buttons: %+v", edit.message.Actions)
	}

	// The endpoint keeps its contract: nothing is pending any more.
	status, body = apiRequest(t, http.MethodPost, server.URL+"/api/v1/reminders/"+reminder.ID+"/complete", "token-alice", "")
	requireStatus(t, status, http.StatusConflict, body)
}

// TestSchedulerRunTicksOnTrigger covers the loop: with a one-hour interval only
// the explicit trigger can fire the reminder, which is what tests and operators
// use to force a tick.
func TestSchedulerRunTicksOnTrigger(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	reminder := env.dueReminder(principal, validInput())

	scheduler := reminders.NewScheduler(env.service, reminders.SchedulerConfig{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	scheduler.Trigger()

	deadline := time.Now().Add(10 * time.Second)
	for env.delivery.sentCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the triggered tick did not fire the reminder")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := env.occurrenceCount(reminder.ID); got != 1 {
		t.Fatalf("occurrences = %d, want 1", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// equalStrings compares two string slices.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
