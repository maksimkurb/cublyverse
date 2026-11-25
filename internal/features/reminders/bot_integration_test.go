package reminders_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	unibot "github.com/maksimkurb/cublyverse/unibot"
	identitymemory "github.com/maksimkurb/cublyverse/unibot/identity/memory"
)

// remoteUserID is the messenger account the bot tests speak as.
const remoteUserID = "42"

// ackRecord is one acknowledgement the transport was asked to show.
type ackRecord struct {
	text  string
	alert bool
}

// botTransport is a messenger that records what the runtime sends and
// acknowledges.
type botTransport struct {
	events chan unibot.Event
	acks   chan ackRecord
	sent   chan unibot.Message
}

func newBotTransport() *botTransport {
	return &botTransport{
		events: make(chan unibot.Event, 8),
		acks:   make(chan ackRecord, 8),
		sent:   make(chan unibot.Message, 8),
	}
}

func (t *botTransport) Platform() unibot.Platform { return unibot.PlatformTelegram }

func (t *botTransport) Capabilities() unibot.Capabilities {
	return unibot.Capabilities{Text: true, NativeActions: true}
}

func (t *botTransport) Start(context.Context) error { return nil }
func (t *botTransport) Close() error                { return nil }
func (t *botTransport) Events() <-chan unibot.Event { return t.events }

func (t *botTransport) Ack(_ context.Context, _ string, text string, alert bool) error {
	t.acks <- ackRecord{text: text, alert: alert}
	return nil
}

func (t *botTransport) Send(_ context.Context, address unibot.Address, message unibot.Message) (unibot.SentMessage, error) {
	t.sent <- message
	return unibot.SentMessage{
		Platform:        unibot.PlatformTelegram,
		RemoteUserID:    address.RemoteUserID,
		ConversationID:  address.ConversationID,
		RemoteMessageID: "bot-message",
	}, nil
}

func (t *botTransport) Edit(context.Context, unibot.SentMessage, unibot.Message) error {
	return nil
}

func (t *botTransport) Delete(context.Context, unibot.SentMessage) error { return nil }

// botHarness runs the reminders router behind a real runtime.
type botHarness struct {
	transport *botTransport
	runtime   *unibot.Runtime
}

// newBotHarness builds a router with the reminders feature and binds principal
// to the Telegram account of the tests.
func newBotHarness(t *testing.T, env *testEnv, principal string) *botHarness {
	t.Helper()

	router := bot.NewRouter(bot.WithWebAppURL("https://cublyverse.example.com/app/"))
	reminders.NewFeature(env.service, slog.New(slog.NewTextHandler(io.Discard, nil))).RegisterBot(router)

	identity := identitymemory.New()
	if err := identity.Bind(context.Background(), unibot.Binding{
		PrincipalID:    unibot.PrincipalID(principal),
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   remoteUserID,
		ConversationID: remoteUserID,
	}); err != nil {
		t.Fatalf("bind identity: %v", err)
	}

	transport := newBotTransport()
	runtime, err := unibot.NewRuntimeWithConfig(unibot.RuntimeConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, identity, router.Handle, transport)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	return &botHarness{transport: transport, runtime: runtime}
}

// press sends a pressed button and returns the acknowledgement.
func (h *botHarness) press(t *testing.T, actionID, payload string) ackRecord {
	t.Helper()
	h.transport.events <- unibot.Event{
		Kind:           unibot.EventAction,
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   remoteUserID,
		ConversationID: remoteUserID,
		ReceivedAt:     time.Now(),
		AckToken:       "callback-1",
		Action:         &unibot.ActionEvent{ActionID: actionID, Payload: payload},
	}
	select {
	case record := <-h.transport.acks:
		return record
	case <-time.After(5 * time.Second):
		t.Fatal("the bot did not acknowledge the button")
		return ackRecord{}
	}
}

// command sends a text command and returns the answer.
func (h *botHarness) command(t *testing.T, text string) unibot.Message {
	t.Helper()
	message := &unibot.IncomingMessage{Text: text}
	if command, ok := unibot.ParseCommand(text); ok {
		message.Command = &command
	}
	h.transport.events <- unibot.Event{
		Kind:           unibot.EventMessage,
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   remoteUserID,
		ConversationID: remoteUserID,
		ReceivedAt:     time.Now(),
		Message:        message,
	}
	select {
	case sent := <-h.transport.sent:
		return sent
	case <-time.After(5 * time.Second):
		t.Fatal("the bot did not answer")
		return unibot.Message{}
	}
}

// TestReminderBotConfirmationBranches covers every answer of the «✅ Выполнено»
// button.
func TestReminderBotConfirmationBranches(t *testing.T) {
	env := newTestEnv(t)
	alice := env.createUser("alice")
	bob := env.createUser("bob")
	h := newBotHarness(t, env, alice)
	ctx := context.Background()

	// The first branch needs a fired reminder of Alice.
	reminder := env.dueReminder(alice, validInput())
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	occurrence := env.activeOccurrence(reminder.ID)
	payload := reminder.ID + ":" + formatTestUUID(occurrence.ID)

	t.Run("completed", func(t *testing.T) {
		record := h.press(t, reminders.ActionDone, payload)
		if record.text != "Подтверждено ✅" || record.alert {
			t.Fatalf("ack = %+v, want «Подтверждено ✅» without an alert", record)
		}
		if _, ok := env.delivery.lastEdit(); !ok {
			t.Fatal("the delivered message was not edited")
		}
		if status := env.occurrenceStatus(occurrence.ID); status != "completed" {
			t.Fatalf("occurrence status = %q, want completed", status)
		}
	})

	t.Run("already", func(t *testing.T) {
		record := h.press(t, reminders.ActionDone, payload)
		if record.text != "Уже подтверждено ✅" || record.alert {
			t.Fatalf("ack = %+v, want «Уже подтверждено ✅»", record)
		}
	})

	t.Run("stale", func(t *testing.T) {
		staleReminder := env.dueReminder(alice, validInput())
		if err := env.scheduler().Tick(ctx); err != nil {
			t.Fatalf("Tick: %v", err)
		}
		staleOccurrence := env.activeOccurrence(staleReminder.ID)
		// Editing the rule cancels the firing.
		if _, err := env.service.Update(ctx, alice, staleReminder.ID, reminders.Input{
			Message:       "Новое правило",
			Timezone:      "Europe/Moscow",
			Kind:          reminders.Weekly,
			Days:          []int{2},
			TimeHHMM:      "18:00",
			RepeatMinutes: 15,
			Enabled:       true,
		}); err != nil {
			t.Fatalf("Update: %v", err)
		}

		record := h.press(t, reminders.ActionDone, staleReminder.ID+":"+formatTestUUID(staleOccurrence.ID))
		if record.text != "Это уже неактуальное напоминание" || record.alert {
			t.Fatalf("ack = %+v, want the stale hint", record)
		}
	})

	t.Run("not owner", func(t *testing.T) {
		bobReminder := env.dueReminder(bob, validInput())
		if err := env.scheduler().Tick(ctx); err != nil {
			t.Fatalf("Tick: %v", err)
		}
		bobOccurrence := env.activeOccurrence(bobReminder.ID)

		// Alice's messenger account presses Bob's button, which is what a leaked
		// payload looks like.
		record := h.press(t, reminders.ActionDone, bobReminder.ID+":"+formatTestUUID(bobOccurrence.ID))
		if record.text != "Это напоминание не для вас" || !record.alert {
			t.Fatalf("ack = %+v, want the foreign-reminder alert", record)
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		tests := []struct{ name, payload string }{
			{name: "unknown ids", payload: "00000000-0000-0000-0000-000000000001:00000000-0000-0000-0000-000000000002"},
			{name: "malformed payload", payload: "garbage"},
		}
		for _, tt := range tests {
			record := h.press(t, reminders.ActionDone, tt.payload)
			if record.text != "Напоминание уже недоступно" || record.alert {
				t.Fatalf("%s: ack = %+v, want «Напоминание уже недоступно»", tt.name, record)
			}
		}
	})

	t.Run("unknown reminders action", func(t *testing.T) {
		record := h.press(t, "reminders.something", payload)
		if record.text != "Это уже неактуальное напоминание" || record.alert {
			t.Fatalf("ack = %+v, want the stale hint", record)
		}
	})
}

// TestReminderListCommand covers /reminders: an empty list, the entries and the
// WebApp button.
func TestReminderListCommand(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	h := newBotHarness(t, env, principal)
	ctx := context.Background()

	message := h.command(t, "/reminders")
	if !strings.Contains(message.Text, "Напоминаний пока нет") {
		t.Fatalf("text = %q, want the empty-list hint", message.Text)
	}
	if len(message.Actions) == 0 {
		t.Fatal("the empty answer has no WebApp button")
	}

	if _, err := env.service.Create(ctx, principal, validInput()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := env.service.Create(ctx, principal, reminders.Input{
		Message:       "Оплатить счёт",
		Timezone:      "Europe/Moscow",
		Kind:          reminders.Monthly,
		Days:          []int{15, 31},
		TimeHHMM:      "18:30",
		RepeatMinutes: 10,
		Enabled:       false,
	}); err != nil {
		t.Fatalf("Create monthly: %v", err)
	}

	message = h.command(t, "/reminders")
	want := []string{
		"Ваши напоминания:",
		"• каждую неделю по пн, ср, пт в 12:00 — Выпить таблетку",
		"• каждый месяц 15 и 31 числа в 18:30 — Оплатить счёт (выключено)",
	}
	for _, part := range want {
		if !strings.Contains(message.Text, part) {
			t.Errorf("text %q does not contain %q", message.Text, part)
		}
	}
	if len(message.Actions) != 1 || message.Actions[0][0].Label != "Открыть приложение" {
		t.Fatalf("actions = %+v, want the WebApp button", message.Actions)
	}

	// More than twenty reminders are cut with a counter.
	for i := 0; i < 21; i++ {
		if _, err := env.service.Create(ctx, principal, validInput()); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}
	message = h.command(t, "/reminders")
	if !strings.Contains(message.Text, "…и ещё 3") {
		t.Fatalf("text = %q, want the cut counter", message.Text)
	}
}

// TestHelpListsRemindersCommand is the /help requirement of M3-4: the feature
// command shows up in the core help without the core knowing about it.
func TestHelpListsRemindersCommand(t *testing.T) {
	env := newTestEnv(t)
	principal := env.createUser("alice")
	h := newBotHarness(t, env, principal)

	message := h.command(t, "/help")
	if !strings.Contains(message.Text, "/reminders — список напоминаний") {
		t.Fatalf("help text = %q, want the /reminders entry", message.Text)
	}
}
