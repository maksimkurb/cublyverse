package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
	interactionmemory "github.com/maksimkurb/cublyverse/unibot/interaction/memory"
)

// apiCall is one request the adapter sent to the Bot API.
type apiCall struct {
	method string
	body   map[string]any
}

// fakeBotAPI answers Telegram Bot API methods and records the calls.
type fakeBotAPI struct {
	mu     sync.Mutex
	calls  []apiCall
	fail   map[string]bool
	server *httptest.Server
}

func newFakeBotAPI(t *testing.T) *fakeBotAPI {
	t.Helper()
	api := &fakeBotAPI{fail: map[string]bool{}}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	api.server = server
	return api
}

func (a *fakeBotAPI) serverURL() string { return a.server.URL }

func (a *fakeBotAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := path.Base(r.URL.Path)
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	shouldFail := a.fail[method]
	a.calls = append(a.calls, apiCall{method: method, body: body})
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if shouldFail {
		_, _ = w.Write([]byte(`{"ok":false,"description":"method is not available"}`))
		return
	}
	switch method {
	case "getUpdates":
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	case "sendMessage", "editMessageText":
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":100,"chat":{"id":42,"type":"private"}}}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}
}

func (a *fakeBotAPI) setFailure(method string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fail[method] = true
}

func (a *fakeBotAPI) callsOf(method string) []apiCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []apiCall
	for _, call := range a.calls {
		if call.method == method {
			out = append(out, call)
		}
	}
	return out
}

func (a *fakeBotAPI) waitForCall(t *testing.T, method string) apiCall {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if calls := a.callsOf(method); len(calls) > 0 {
			return calls[len(calls)-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("telegram method %s was never called", method)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTestAdapter(t *testing.T, api *fakeBotAPI, store unibot.InteractionStore) *Adapter {
	t.Helper()
	if store == nil {
		store = interactionmemory.New()
	}
	adapter, err := New(Config{
		Token:      "test-token",
		BaseURL:    api.serverURL(),
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter
}

func privateAddress() unibot.Address {
	return unibot.Address{Platform: unibot.PlatformTelegram, RemoteUserID: "42", ConversationID: "42"}
}

// TestSendHTMLEscaping covers M2-1: HTML travels as parse_mode=HTML, plain text
// travels as-is.
func TestSendHTMLEscaping(t *testing.T) {
	tests := []struct {
		name          string
		message       unibot.Message
		wantText      string
		wantParseMode string
	}{
		{
			name:     "plain text",
			message:  unibot.Message{Text: "Привет <b>мир</b>"},
			wantText: "Привет <b>мир</b>",
		},
		{
			name:          "html",
			message:       unibot.Message{Text: "Первое напоминание", HTML: "<b>Первое напоминание</b>"},
			wantText:      "<b>Первое напоминание</b>",
			wantParseMode: "HTML",
		},
		{
			name:          "html only",
			message:       unibot.Message{HTML: "Готово <i>сегодня</i>"},
			wantText:      "Готово <i>сегодня</i>",
			wantParseMode: "HTML",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeBotAPI(t)
			adapter := newTestAdapter(t, api, nil)

			sent, err := adapter.Send(context.Background(), privateAddress(), tt.message)
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if sent.RemoteMessageID != "100" {
				t.Errorf("message id = %q, want 100", sent.RemoteMessageID)
			}
			call := api.waitForCall(t, "sendMessage")
			if got := call.body["text"]; got != tt.wantText {
				t.Errorf("text = %v, want %q", got, tt.wantText)
			}
			parseMode, hasParseMode := call.body["parse_mode"]
			if tt.wantParseMode == "" {
				if hasParseMode {
					t.Errorf("parse_mode = %v, want none", parseMode)
				}
				return
			}
			if parseMode != tt.wantParseMode {
				t.Errorf("parse_mode = %v, want %q", parseMode, tt.wantParseMode)
			}
		})
	}
}

func TestEditHTML(t *testing.T) {
	api := newFakeBotAPI(t)
	adapter := newTestAdapter(t, api, nil)

	sent := unibot.SentMessage{
		Platform:        unibot.PlatformTelegram,
		RemoteUserID:    "42",
		ConversationID:  "42",
		RemoteMessageID: "100",
	}
	err := adapter.Edit(context.Background(), sent, unibot.Message{HTML: "<b>✅ Выполнено</b>"})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	call := api.waitForCall(t, "editMessageText")
	if call.body["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %v, want HTML", call.body["parse_mode"])
	}
	if call.body["text"] != "<b>✅ Выполнено</b>" {
		t.Errorf("text = %v", call.body["text"])
	}
}

// TestActionTTLAndPayload covers M2-3: Action.TTL and Action.Payload reach the
// interaction store.
func TestActionTTLAndPayload(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
		want time.Duration
	}{
		{name: "default ttl", ttl: 0, want: unibot.DefaultActionTTL},
		{name: "custom ttl", ttl: 2 * time.Hour, want: 2 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeBotAPI(t)
			store := interactionmemory.New()
			adapter := newTestAdapter(t, api, store)

			_, err := adapter.Send(context.Background(), privateAddress(), unibot.Message{
				Text: "Напоминание",
				Actions: [][]unibot.Action{{{
					ID:          "reminders.done",
					Label:       "Готово",
					Kind:        unibot.ActionSubmit,
					Payload:     "reminder-1:occurrence-2",
					PrincipalID: "00000000-0000-0000-0000-000000000001",
					TTL:         tt.ttl,
				}}},
			})
			if err != nil {
				t.Fatalf("Send: %v", err)
			}

			call := api.waitForCall(t, "sendMessage")
			markup, ok := call.body["reply_markup"].(map[string]any)
			if !ok {
				t.Fatalf("reply_markup = %v", call.body["reply_markup"])
			}
			rows, ok := markup["inline_keyboard"].([]any)
			if !ok || len(rows) != 1 {
				t.Fatalf("inline_keyboard = %v", markup["inline_keyboard"])
			}
			row, _ := rows[0].([]any)
			button, _ := row[0].(map[string]any)
			data, _ := button["callback_data"].(string)
			if !strings.HasPrefix(data, "u:") {
				t.Fatalf("callback_data = %q, want a token", data)
			}

			interaction, found, err := store.ResolveToken(context.Background(), strings.TrimPrefix(data, "u:"))
			if err != nil {
				t.Fatalf("ResolveToken: %v", err)
			}
			if !found {
				t.Fatal("the interaction was not stored")
			}
			if interaction.ActionID != "reminders.done" || interaction.Payload != "reminder-1:occurrence-2" {
				t.Fatalf("interaction = %+v", interaction)
			}
			if got := time.Until(interaction.ExpiresAt); got < tt.want-2*time.Second || got > tt.want+2*time.Second {
				t.Fatalf("ttl = %v, want about %v", got, tt.want)
			}
			if interaction.ExpectedUserID != "42" {
				t.Errorf("expected user = %q, want 42", interaction.ExpectedUserID)
			}
			if interaction.PrincipalID != "00000000-0000-0000-0000-000000000001" {
				t.Errorf("principal = %q, want the action restriction", interaction.PrincipalID)
			}
		})
	}
}

// TestCallbackQueryIsNotAcknowledgedImmediately covers M2-2: the adapter only
// publishes the event with an AckToken, the runtime or the handler answers.
func TestCallbackQueryIsNotAcknowledgedImmediately(t *testing.T) {
	api := newFakeBotAPI(t)
	store := interactionmemory.New()
	adapter := newTestAdapter(t, api, store)
	ctx := context.Background()

	if err := store.PutToken(ctx, "token-1", unibot.Interaction{
		ActionID:       "reminders.done",
		Payload:        "reminder-1:occurrence-2",
		ExpectedUserID: "42",
		PrincipalID:    "00000000-0000-0000-0000-000000000001",
		ExpiresAt:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutToken: %v", err)
	}

	adapter.handleUpdate(ctx, tgUpdate{CallbackQuery: &tgCallbackQuery{
		ID:      "cb-1",
		From:    tgUser{ID: 42},
		Message: &tgMessage{Chat: tgChat{ID: 42, Type: "private"}},
		Data:    "u:token-1",
	}})

	select {
	case event := <-adapter.Events():
		if event.Kind != unibot.EventAction {
			t.Fatalf("kind = %q, want action", event.Kind)
		}
		if event.AckToken != "cb-1" {
			t.Errorf("ack token = %q, want cb-1", event.AckToken)
		}
		if event.Action.ActionID != "reminders.done" {
			t.Errorf("action id = %q", event.Action.ActionID)
		}
		if event.Action.Payload != "reminder-1:occurrence-2" {
			t.Errorf("payload = %q", event.Action.Payload)
		}
		if event.Action.PrincipalID != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("principal = %q", event.Action.PrincipalID)
		}
	case <-time.After(time.Second):
		t.Fatal("the callback query produced no event")
	}

	if calls := api.callsOf("answerCallbackQuery"); len(calls) != 0 {
		t.Fatalf("the adapter answered the callback immediately: %+v", calls)
	}
}

// TestAck covers botctx.Ack on the transport level.
func TestAck(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		alert     bool
		wantText  any
		wantAlert any
	}{
		{name: "empty ack", wantText: nil, wantAlert: nil},
		{name: "toast", text: "Подтверждено ✅", wantText: "Подтверждено ✅", wantAlert: nil},
		{name: "alert", text: "Это напоминание не для вас", alert: true, wantText: "Это напоминание не для вас", wantAlert: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeBotAPI(t)
			adapter := newTestAdapter(t, api, nil)

			if err := adapter.Ack(context.Background(), "cb-1", tt.text, tt.alert); err != nil {
				t.Fatalf("Ack: %v", err)
			}
			call := api.waitForCall(t, "answerCallbackQuery")
			if call.body["callback_query_id"] != "cb-1" {
				t.Errorf("callback_query_id = %v", call.body["callback_query_id"])
			}
			got, exists := call.body["text"]
			if tt.wantText == nil {
				if exists {
					t.Errorf("text = %v, want absent", got)
				}
			} else if got != tt.wantText {
				t.Errorf("text = %v, want %v", got, tt.wantText)
			}
			if got, exists := call.body["show_alert"]; tt.alert {
				if !exists || got != true {
					t.Errorf("show_alert = %v, want true", got)
				}
			} else if exists {
				t.Errorf("show_alert = %v, want absent", got)
			}
		})
	}
}

// TestCallbackQueryFromGroupIsIgnored checks the private-chat rule for buttons.
func TestCallbackQueryFromGroupIsIgnored(t *testing.T) {
	api := newFakeBotAPI(t)
	store := interactionmemory.New()
	adapter := newTestAdapter(t, api, store)
	ctx := context.Background()

	if err := store.PutToken(ctx, "token-1", unibot.Interaction{
		ActionID:       "reminders.done",
		ExpectedUserID: "42",
		ExpiresAt:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutToken: %v", err)
	}

	adapter.handleUpdate(ctx, tgUpdate{CallbackQuery: &tgCallbackQuery{
		ID:      "cb-1",
		From:    tgUser{ID: 42},
		Message: &tgMessage{Chat: tgChat{ID: -100, Type: "group"}},
		Data:    "u:token-1",
	}})

	select {
	case event := <-adapter.Events():
		t.Fatalf("group callback produced an event: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
	// The spinner is stopped, but nothing is dispatched.
	call := api.waitForCall(t, "answerCallbackQuery")
	if _, exists := call.body["text"]; exists {
		t.Errorf("group callback got text %v, want an empty ack", call.body["text"])
	}
}

// TestUnknownCallbackTokenIsAcknowledged checks that an expired button still
// stops the client spinner.
func TestUnknownCallbackTokenIsAcknowledged(t *testing.T) {
	api := newFakeBotAPI(t)
	adapter := newTestAdapter(t, api, nil)

	adapter.handleUpdate(context.Background(), tgUpdate{CallbackQuery: &tgCallbackQuery{
		ID:      "cb-1",
		From:    tgUser{ID: 42},
		Message: &tgMessage{Chat: tgChat{ID: 42, Type: "private"}},
		Data:    "u:missing",
	}})

	select {
	case event := <-adapter.Events():
		t.Fatalf("unknown token produced an event: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
	api.waitForCall(t, "answerCallbackQuery")
}

// TestNonPrivateMessagesAreIgnored covers the "only private chats" rule.
func TestNonPrivateMessagesAreIgnored(t *testing.T) {
	tests := []struct {
		name     string
		chatType string
		want     bool
	}{
		{name: "private", chatType: "private", want: true},
		{name: "group", chatType: "group"},
		{name: "supergroup", chatType: "supergroup"},
		{name: "channel", chatType: "channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestAdapter(t, newFakeBotAPI(t), nil)
			adapter.handleUpdate(context.Background(), tgUpdate{Message: &tgMessage{
				MessageID: 7,
				From:      &tgUser{ID: 42},
				Chat:      tgChat{ID: -100, Type: tt.chatType},
				Text:      "привет",
			}})

			select {
			case event := <-adapter.Events():
				if !tt.want {
					t.Fatalf("chat type %q produced an event: %+v", tt.chatType, event)
				}
			case <-time.After(100 * time.Millisecond):
				if tt.want {
					t.Fatalf("chat type %q produced no event", tt.chatType)
				}
			}
		})
	}
}

// TestCommandParsing checks that the adapter exposes /cmd@bot args.
func TestCommandParsing(t *testing.T) {
	tests := []struct {
		name string
		text string
		want *unibot.Command
	}{
		{name: "start", text: "/start", want: &unibot.Command{Name: "start"}},
		{name: "addressed command", text: "/reminders@cublyverse_bot 7", want: &unibot.Command{Name: "reminders", Addressee: "cublyverse_bot", Args: "7"}},
		{name: "plain text", text: "напоминание"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestAdapter(t, newFakeBotAPI(t), nil)
			adapter.handleUpdate(context.Background(), tgUpdate{Message: &tgMessage{
				MessageID: 7,
				From:      &tgUser{ID: 42},
				Chat:      tgChat{ID: 42, Type: "private"},
				Text:      tt.text,
			}})

			select {
			case event := <-adapter.Events():
				got := event.Message.Command
				if (tt.want == nil) != (got == nil) {
					t.Fatalf("command = %+v, want %+v", got, tt.want)
				}
				if tt.want != nil && *got != *tt.want {
					t.Fatalf("command = %+v, want %+v", got, tt.want)
				}
			case <-time.After(time.Second):
				t.Fatal("the message produced no event")
			}
		})
	}
}

// TestStartPublishesMenu covers M2-5: setMyCommands and setChatMenuButton run
// on start.
func TestStartPublishesMenu(t *testing.T) {
	api := newFakeBotAPI(t)
	store := interactionmemory.New()
	adapter, err := New(Config{
		Token:      "test-token",
		BaseURL:    api.serverURL(),
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Commands: []Command{
			{Command: "start", Description: "начать работу"},
			{Command: "help", Description: "список команд"},
		},
		WebAppURL: "https://cublyverse.example.com/app/",
	}, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := adapter.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	commandsCall := api.waitForCall(t, "setMyCommands")
	commands, ok := commandsCall.body["commands"].([]any)
	if !ok || len(commands) != 2 {
		t.Fatalf("commands = %v", commandsCall.body["commands"])
	}
	first, _ := commands[0].(map[string]any)
	if first["command"] != "start" || first["description"] != "начать работу" {
		t.Errorf("first command = %v", first)
	}

	menuCall := api.waitForCall(t, "setChatMenuButton")
	menu, ok := menuCall.body["menu_button"].(map[string]any)
	if !ok {
		t.Fatalf("menu_button = %v", menuCall.body["menu_button"])
	}
	if menu["type"] != "web_app" {
		t.Errorf("menu type = %v, want web_app", menu["type"])
	}
	webApp, _ := menu["web_app"].(map[string]any)
	if webApp["url"] != "https://cublyverse.example.com/app/" {
		t.Errorf("web_app = %v", webApp)
	}
}

// TestSetupFailuresAreNotFatal checks that a broken menu call does not stop the
// adapter.
func TestSetupFailuresAreNotFatal(t *testing.T) {
	api := newFakeBotAPI(t)
	api.setFailure("setMyCommands")
	api.setFailure("setChatMenuButton")

	var logs syncBuffer
	store := interactionmemory.New()
	adapter, err := New(Config{
		Token:      "test-token",
		BaseURL:    api.serverURL(),
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Logger:     slog.New(slog.NewTextHandler(&logs, nil)),
		Commands:   []Command{{Command: "start", Description: "начать работу"}},
		WebAppURL:  "https://cublyverse.example.com/app/",
	}, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := adapter.Start(ctx); err != nil {
		t.Fatalf("Start returned %v, want nil: the bot works without a menu", err)
	}
	defer func() { _ = adapter.Close() }()

	api.waitForCall(t, "setMyCommands")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "setMyCommands failed") {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want a setMyCommands warning", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
