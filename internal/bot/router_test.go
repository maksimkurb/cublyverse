package bot_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/bot"
	unibot "github.com/maksimkurb/cublyverse/unibot"
	identitymemory "github.com/maksimkurb/cublyverse/unibot/identity/memory"
)

// recordingTransport is a messenger that records what the bot sends.
type recordingTransport struct {
	events chan unibot.Event
	sent   chan unibot.Message
}

func newRecordingTransport() *recordingTransport {
	return &recordingTransport{
		events: make(chan unibot.Event, 8),
		sent:   make(chan unibot.Message, 8),
	}
}

func (t *recordingTransport) Platform() unibot.Platform { return unibot.PlatformTelegram }
func (t *recordingTransport) Capabilities() unibot.Capabilities {
	return unibot.Capabilities{Text: true, NativeActions: true}
}
func (t *recordingTransport) Start(context.Context) error { return nil }
func (t *recordingTransport) Close() error                { return nil }
func (t *recordingTransport) Events() <-chan unibot.Event { return t.events }
func (t *recordingTransport) Send(_ context.Context, address unibot.Address, message unibot.Message) (unibot.SentMessage, error) {
	t.sent <- message
	return unibot.SentMessage{
		Platform:        t.Platform(),
		RemoteUserID:    address.RemoteUserID,
		ConversationID:  address.ConversationID,
		RemoteMessageID: "1",
	}, nil
}
func (t *recordingTransport) Edit(context.Context, unibot.SentMessage, unibot.Message) error {
	return nil
}
func (t *recordingTransport) Delete(context.Context, unibot.SentMessage) error { return nil }

// links is a stub of the sign-in linker.
type links struct {
	url string
	err error

	mu      sync.Mutex
	calls   int
	address unibot.Address
}

func (l *links) BeginMessengerBinding(_ context.Context, platform unibot.Platform, remoteUserID, conversationID string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.address = unibot.Address{Platform: platform, RemoteUserID: remoteUserID, ConversationID: conversationID}
	return l.url, l.err
}

// harness runs a router behind a real runtime with one recording transport.
type harness struct {
	transport *recordingTransport
	runtime   *unibot.Runtime
}

func newHarness(t *testing.T, router *bot.Router, linked bool) *harness {
	t.Helper()
	transport := newRecordingTransport()
	identity := identitymemory.New()
	if linked {
		if err := identity.Bind(context.Background(), unibot.Binding{
			PrincipalID:    "principal-1",
			Platform:       unibot.PlatformTelegram,
			RemoteUserID:   "42",
			ConversationID: "42",
		}); err != nil {
			t.Fatalf("Bind: %v", err)
		}
	}
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
	return &harness{transport: transport, runtime: runtime}
}

// send delivers one event and returns the answer of the bot.
func (h *harness) send(t *testing.T, event unibot.Event) unibot.Message {
	t.Helper()
	event.Platform = unibot.PlatformTelegram
	event.RemoteUserID = "42"
	event.ConversationID = "42"
	event.ReceivedAt = time.Now()
	h.transport.events <- event
	select {
	case message := <-h.transport.sent:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("the bot sent no message")
		return unibot.Message{}
	}
}

func textEvent(text string) unibot.Event {
	message := &unibot.IncomingMessage{Text: text}
	if command, ok := unibot.ParseCommand(text); ok {
		message.Command = &command
	}
	return unibot.Event{Kind: unibot.EventMessage, Message: message}
}

func actionEvent(actionID string) unibot.Event {
	return unibot.Event{Kind: unibot.EventAction, Action: &unibot.ActionEvent{ActionID: actionID}}
}

func TestUnlinkedAccountGetsLoginButton(t *testing.T) {
	linker := &links{url: "https://cublyverse.example.com/auth/login?state=abc"}
	router := bot.NewRouter(bot.WithLoginLinks(linker), bot.WithWebAppURL("https://cublyverse.example.com/app/"))
	h := newHarness(t, router, false)

	message := h.send(t, textEvent("привет"))
	if len(message.Actions) != 1 || len(message.Actions[0]) != 1 {
		t.Fatalf("actions = %+v, want one button", message.Actions)
	}
	action := message.Actions[0][0]
	if action.Kind != unibot.ActionOpenApp {
		t.Errorf("action kind = %q, want open_app", action.Kind)
	}
	if action.URL != linker.url {
		t.Errorf("action URL = %q, want %q", action.URL, linker.url)
	}
	if action.Label != "Войти" {
		t.Errorf("action label = %q, want Войти", action.Label)
	}
	if linker.calls != 1 {
		t.Errorf("linker calls = %d, want 1", linker.calls)
	}
	if linker.address.RemoteUserID != "42" || linker.address.ConversationID != "42" {
		t.Errorf("linker address = %+v", linker.address)
	}
}

func TestLoginLinkFailureIsReported(t *testing.T) {
	router := bot.NewRouter(bot.WithLoginLinks(&links{err: errors.New("database is down")}))
	h := newHarness(t, router, false)

	message := h.send(t, textEvent("привет"))
	if len(message.Actions) != 0 {
		t.Fatalf("actions = %+v, want a plain error message", message.Actions)
	}
	if message.Text == "" {
		t.Fatal("the bot answered with an empty message")
	}
}

func TestLinkedAccountFallback(t *testing.T) {
	tests := []struct {
		name      string
		webAppURL string
		wantURL   string
	}{
		{name: "with WebApp URL", webAppURL: "https://cublyverse.example.com/app/", wantURL: "https://cublyverse.example.com/app/"},
		{name: "without WebApp URL", webAppURL: "", wantURL: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linker := &links{err: errors.New("must not be called")}
			router := bot.NewRouter(bot.WithLoginLinks(linker), bot.WithWebAppURL(tt.webAppURL))
			h := newHarness(t, router, true)

			message := h.send(t, textEvent("привет"))
			if tt.wantURL == "" {
				if len(message.Actions) != 0 {
					t.Fatalf("actions = %+v, want none", message.Actions)
				}
			} else if len(message.Actions) != 1 || message.Actions[0][0].URL != tt.wantURL {
				t.Fatalf("actions = %+v, want the WebApp button", message.Actions)
			}
			if linker.calls != 0 {
				t.Errorf("linker was called %d times for a linked account", linker.calls)
			}
		})
	}
}

func TestStartCommand(t *testing.T) {
	router := bot.NewRouter(bot.WithWebAppURL("https://cublyverse.example.com/app/"))
	h := newHarness(t, router, true)

	message := h.send(t, textEvent("/start"))
	if !strings.Contains(message.Text, "Cublyverse") {
		t.Fatalf("text = %q, want a greeting", message.Text)
	}
	if len(message.Actions) != 1 || message.Actions[0][0].Label != "Открыть приложение" {
		t.Fatalf("actions = %+v, want the WebApp button", message.Actions)
	}
}

func TestHelpListsRegisteredCommands(t *testing.T) {
	router := bot.NewRouter()
	router.Command("reminders", "список напоминаний", func(context.Context, *unibot.Context, unibot.ResolvedEvent) error {
		return nil
	})
	h := newHarness(t, router, true)

	message := h.send(t, textEvent("/help"))
	for _, want := range []string{"/start", "/help", "/reminders — список напоминаний"} {
		if !strings.Contains(message.Text, want) {
			t.Errorf("help text %q does not contain %q", message.Text, want)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	h := newHarness(t, bot.NewRouter(), true)

	message := h.send(t, textEvent("/nonexistent"))
	if !strings.Contains(message.Text, "/help") {
		t.Fatalf("text = %q, want a hint about /help", message.Text)
	}
}

func TestCommandAddressedToBot(t *testing.T) {
	router := bot.NewRouter()
	called := make(chan struct{}, 1)
	router.Command("start", "начать работу", func(context.Context, *unibot.Context, unibot.ResolvedEvent) error {
		called <- struct{}{}
		return nil
	})
	h := newHarness(t, router, true)

	// The handler sends nothing, so wait for it to run.
	event := textEvent("/start@cublyverse_bot")
	event.Platform = unibot.PlatformTelegram
	event.RemoteUserID = "42"
	event.ConversationID = "42"
	event.ReceivedAt = time.Now()
	h.transport.events <- event

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("the addressed command was not dispatched")
	}
}

func TestActionPrefixRouting(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		want    string
		wantHit bool
	}{
		{name: "prefix match", action: "reminders.done:1:2", want: "generic"},
		{name: "longest prefix wins", action: "reminders.done:1:2", want: "done"},
		{name: "no match", action: "unknown.thing", want: "stale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := bot.NewRouter()
			var hit string
			reply := func(name string) bot.Handler {
				return func(ctx context.Context, botctx *unibot.Context, _ unibot.ResolvedEvent) error {
					hit = name
					_, err := botctx.Reply(ctx, unibot.Message{Text: name})
					return err
				}
			}
			router.Action("reminders.", reply("generic"))
			if tt.want == "done" {
				router.Action("reminders.done:", reply("done"))
			}
			h := newHarness(t, router, true)

			message := h.send(t, actionEvent(tt.action))
			switch tt.want {
			case "stale":
				if !strings.Contains(message.Text, "неактуально") {
					t.Fatalf("text = %q, want the stale action hint", message.Text)
				}
			default:
				if hit != tt.want {
					t.Fatalf("handler = %q, want %q", hit, tt.want)
				}
			}
		})
	}
}

func TestCommandsInRegistrationOrder(t *testing.T) {
	router := bot.NewRouter()
	router.Command("reminders", "напоминания", func(context.Context, *unibot.Context, unibot.ResolvedEvent) error {
		return nil
	})
	router.Command("notes", "заметки", func(context.Context, *unibot.Context, unibot.ResolvedEvent) error {
		return nil
	})
	router.Command("/START", "дубль", func(context.Context, *unibot.Context, unibot.ResolvedEvent) error {
		return nil
	})

	commands := router.Commands()
	want := []string{"start", "help", "reminders", "notes"}
	if len(commands) != len(want) {
		t.Fatalf("Commands() = %+v, want %v", commands, want)
	}
	for i, name := range want {
		if commands[i].Name != name {
			t.Errorf("Commands()[%d] = %q, want %q", i, commands[i].Name, name)
		}
	}
	if commands[0].Description != "дубль" {
		t.Errorf("re-registration did not replace the description: %q", commands[0].Description)
	}
}
