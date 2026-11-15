package bot_test

import (
	"context"
	"errors"
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

// receive waits for one message the bot sent.
func receive(t *testing.T, transport *recordingTransport) unibot.Message {
	t.Helper()
	select {
	case message := <-transport.sent:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("the bot sent no message")
		return unibot.Message{}
	}
}

func TestUnlinkedAccountGetsLoginButton(t *testing.T) {
	transport := newRecordingTransport()
	linker := &links{url: "https://cublyverse.example.com/auth/login?state=abc"}
	handler := &bot.Handler{Links: linker, WebAppURL: "https://cublyverse.example.com/app/"}

	runtime, err := unibot.NewRuntime(identitymemory.New(), handler.Handle, transport)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	transport.events <- unibot.Event{
		Kind:           unibot.EventMessage,
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   "42",
		ConversationID: "42",
		ReceivedAt:     time.Now(),
		Message:        &unibot.IncomingMessage{Text: "привет"},
	}

	message := receive(t, transport)
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

func TestLinkedAccountGetsAppButton(t *testing.T) {
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
			transport := newRecordingTransport()
			identity := identitymemory.New()
			if err := identity.Bind(context.Background(), unibot.Binding{
				PrincipalID:    "principal-1",
				Platform:       unibot.PlatformTelegram,
				RemoteUserID:   "42",
				ConversationID: "42",
			}); err != nil {
				t.Fatalf("Bind: %v", err)
			}
			linker := &links{err: errors.New("must not be called")}
			handler := &bot.Handler{Links: linker, WebAppURL: tt.webAppURL}

			runtime, err := unibot.NewRuntime(identity, handler.Handle, transport)
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := runtime.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer func() { _ = runtime.Close() }()

			transport.events <- unibot.Event{
				Kind:           unibot.EventMessage,
				Platform:       unibot.PlatformTelegram,
				RemoteUserID:   "42",
				ConversationID: "42",
				ReceivedAt:     time.Now(),
				Message:        &unibot.IncomingMessage{Text: "привет"},
			}

			message := receive(t, transport)
			if tt.wantURL == "" {
				if len(message.Actions) != 0 {
					t.Fatalf("actions = %+v, want none", message.Actions)
				}
			} else {
				if len(message.Actions) != 1 || message.Actions[0][0].URL != tt.wantURL {
					t.Fatalf("actions = %+v, want the WebApp button", message.Actions)
				}
			}
			if linker.calls != 0 {
				t.Errorf("linker was called %d times for a linked account", linker.calls)
			}
		})
	}
}

func TestLoginLinkFailureIsReported(t *testing.T) {
	transport := newRecordingTransport()
	handler := &bot.Handler{Links: &links{err: errors.New("database is down")}}

	runtime, err := unibot.NewRuntime(identitymemory.New(), handler.Handle, transport)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	transport.events <- unibot.Event{
		Kind:           unibot.EventMessage,
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   "42",
		ConversationID: "42",
		ReceivedAt:     time.Now(),
		Message:        &unibot.IncomingMessage{Text: "привет"},
	}

	message := receive(t, transport)
	if len(message.Actions) != 0 {
		t.Fatalf("actions = %+v, want a plain error message", message.Actions)
	}
	if message.Text == "" {
		t.Fatal("the bot answered with an empty message")
	}
}
