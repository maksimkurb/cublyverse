package unibot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type testIdentity struct{ bindings []Binding }

func (s *testIdentity) Bind(context.Context, Binding) error { return nil }
func (s *testIdentity) Resolve(context.Context, Platform, string) (PrincipalID, bool, error) {
	return "p", true, nil
}
func (s *testIdentity) Bindings(context.Context, PrincipalID) ([]Binding, error) {
	return append([]Binding(nil), s.bindings...), nil
}

type ackCall struct {
	token string
	text  string
	alert bool
}

type testTransport struct {
	platform Platform
	err      error
	mu       sync.Mutex
	sends    int
	events   chan Event
	acks     []ackCall
}

func (t *testTransport) Platform() Platform         { return t.platform }
func (t *testTransport) Capabilities() Capabilities { return Capabilities{Text: true} }
func (t *testTransport) Start(context.Context) error {
	return nil
}
func (t *testTransport) Close() error { return nil }
func (t *testTransport) Events() <-chan Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.events == nil {
		t.events = make(chan Event)
	}
	return t.events
}
func (t *testTransport) Send(_ context.Context, a Address, _ Message) (SentMessage, error) {
	t.mu.Lock()
	t.sends++
	t.mu.Unlock()
	if t.err != nil {
		return SentMessage{}, t.err
	}
	return SentMessage{Platform: t.platform, RemoteUserID: a.RemoteUserID, ConversationID: a.ConversationID, RemoteMessageID: "1"}, nil
}
func (t *testTransport) Edit(context.Context, SentMessage, Message) error { return nil }
func (t *testTransport) Delete(context.Context, SentMessage) error        { return nil }

func (t *testTransport) Ack(_ context.Context, token, text string, alert bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.acks = append(t.acks, ackCall{token: token, text: text, alert: alert})
	return nil
}

func (t *testTransport) eventChan() chan Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.events == nil {
		t.events = make(chan Event)
	}
	return t.events
}

func (t *testTransport) ackCalls() []ackCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]ackCall(nil), t.acks...)
}

// syncBuffer is a goroutine-safe log sink: the runtime logs from handler
// goroutines while the test reads the output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&syncBuffer{}, nil))
}

func TestSendToPrincipalFallsBack(t *testing.T) {
	identity := &testIdentity{bindings: []Binding{
		{PrincipalID: "p", Platform: PlatformMatrix, RemoteUserID: "@u:x", ConversationID: "!r:x"},
		{PrincipalID: "p", Platform: PlatformTelegram, RemoteUserID: "42", ConversationID: "42"},
	}}
	tg := &testTransport{platform: PlatformTelegram, err: errors.New("down")}
	mx := &testTransport{platform: PlatformMatrix}
	r, err := NewRuntime(identity, func(context.Context, *Context, ResolvedEvent) error { return nil }, tg, mx)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sent, err := r.SendToPrincipal(ctx, "p", Message{Text: "hello"}, DeliveryOptions{Preferred: []Platform{PlatformTelegram, PlatformMatrix}})
	if err != nil {
		t.Fatalf("SendToPrincipal: %v", err)
	}
	if len(sent) != 1 || sent[0].Platform != PlatformMatrix {
		t.Fatalf("sent = %#v", sent)
	}
	if tg.sends != 1 || mx.sends != 1 {
		t.Fatalf("send counts telegram=%d matrix=%d", tg.sends, mx.sends)
	}
}

// startTestRuntime starts a runtime over one transport whose events channel is
// created before the consumers read it.
func startTestRuntime(t *testing.T, cfg RuntimeConfig, handler Handler, identity IdentityStore, transport *testTransport) (*Runtime, context.CancelFunc) {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	_ = transport.eventChan() // create the channel before Events() is polled
	runtime, err := NewRuntimeWithConfig(cfg, identity, handler, transport)
	if err != nil {
		t.Fatalf("NewRuntimeWithConfig: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = runtime.Close()
	})
	return runtime, cancel
}

// TestPerUserOrdering is the M2-4 acceptance test: events of one remote user
// are handled one at a time and in order, while different users run in
// parallel.
func TestPerUserOrdering(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}

	var mu sync.Mutex
	got := make(map[string][]int)
	inFlight := make(map[string]int)
	concurrent := 0
	maxConcurrent := 0

	handler := func(_ context.Context, _ *Context, event ResolvedEvent) error {
		mu.Lock()
		inFlight[event.RemoteUserID]++
		concurrent++
		if inFlight[event.RemoteUserID] > 1 {
			mu.Unlock()
			return errors.New("two events of one user ran at the same time")
		}
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()

		time.Sleep(2 * time.Millisecond)

		mu.Lock()
		inFlight[event.RemoteUserID]--
		concurrent--
		got[event.RemoteUserID] = append(got[event.RemoteUserID], event.MessageIndex())
		mu.Unlock()
		return nil
	}

	_, _ = startTestRuntime(t, RuntimeConfig{}, handler, &testIdentity{}, transport)

	const users, perUser = 4, 8
	for i := 0; i < perUser; i++ {
		for u := 0; u < users; u++ {
			transport.eventChan() <- testEvent(PlatformTelegram, string(rune('a'+u)), i)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		total := 0
		for _, sequence := range got {
			total += len(sequence)
		}
		mu.Unlock()
		if total == users*perUser {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("handled %d events of %d", total, users*perUser)
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	for user, sequence := range got {
		if len(sequence) != perUser {
			t.Fatalf("user %q got %d events, want %d", user, len(sequence), perUser)
		}
		for i, index := range sequence {
			if index != i {
				t.Fatalf("user %q handled index %d at position %d, want in order", user, index, i)
			}
		}
	}
	if maxConcurrent < 2 {
		t.Errorf("handlers never ran in parallel: max concurrency %d", maxConcurrent)
	}
}

// testEvent builds a message event whose text carries the sequence number.
func testEvent(platform Platform, remoteUserID string, index int) Event {
	return Event{
		Kind:           EventMessage,
		Platform:       platform,
		RemoteUserID:   remoteUserID,
		ConversationID: remoteUserID,
		ReceivedAt:     time.Now(),
		Message:        &IncomingMessage{Text: string(rune('0' + index))},
	}
}

// MessageIndex reads the sequence number back from the text.
func (e ResolvedEvent) MessageIndex() int {
	if e.Message == nil || e.Message.Text == "" {
		return -1
	}
	return int(e.Message.Text[0] - '0')
}

// TestBoundedQueues checks the small-limit case: with one handler slot and a
// tiny pending buffer every event is still processed, one user at a time.
func TestBoundedQueues(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}

	var mu sync.Mutex
	handled := 0
	concurrent := 0
	maxConcurrent := 0

	handler := func(_ context.Context, _ *Context, _ ResolvedEvent) error {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()

		time.Sleep(time.Millisecond)

		mu.Lock()
		concurrent--
		handled++
		mu.Unlock()
		return nil
	}

	_, _ = startTestRuntime(t, RuntimeConfig{
		MaxConcurrentHandlers: 1,
		MaxPendingEvents:      1,
		QueueSize:             1,
	}, handler, &testIdentity{}, transport)

	const users, perUser = 3, 4
	go func() {
		for i := 0; i < perUser; i++ {
			for u := 0; u < users; u++ {
				transport.eventChan() <- testEvent(PlatformTelegram, string(rune('a'+u)), i)
			}
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		total, peak := handled, maxConcurrent
		mu.Unlock()
		if total == users*perUser {
			if peak != 1 {
				t.Fatalf("max concurrency = %d, want 1", peak)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("handled %d events of %d: the bounded queue deadlocked", total, users*perUser)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestHandlerPanicDoesNotStopTheRuntime checks that a panicking handler is
// logged and the next event is still processed.
func TestHandlerPanicDoesNotStopTheRuntime(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}
	logs := &syncBuffer{}
	handled := make(chan string, 2)

	handler := func(_ context.Context, _ *Context, event ResolvedEvent) error {
		if event.Message.Text == "panic" {
			panic("boom")
		}
		handled <- event.Message.Text
		return nil
	}

	_, _ = startTestRuntime(t, RuntimeConfig{Logger: slog.New(slog.NewTextHandler(logs, nil))}, handler, &testIdentity{}, transport)

	transport.eventChan() <- testTextEvent("panic")
	transport.eventChan() <- testTextEvent("after")

	select {
	case text := <-handled:
		if text != "after" {
			t.Fatalf("handled %q, want the event after the panic", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the runtime stopped after a handler panic")
	}
	if !strings.Contains(logs.String(), "panicked") {
		t.Errorf("log = %q, want a panic entry", logs.String())
	}
}

func testTextEvent(text string) Event {
	return Event{
		Kind:         EventMessage,
		Platform:     PlatformTelegram,
		RemoteUserID: "42",
		ReceivedAt:   time.Now(),
		Message:      &IncomingMessage{Text: text},
	}
}

// TestHandlerTimeout checks the configurable handler timeout: the handler sees
// a cancelled context and the runtime reports the timeout.
func TestHandlerTimeout(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}
	logs := &syncBuffer{}
	observed := make(chan error, 1)

	handler := func(ctx context.Context, _ *Context, _ ResolvedEvent) error {
		<-ctx.Done()
		observed <- ctx.Err()
		return ctx.Err()
	}

	_, _ = startTestRuntime(t, RuntimeConfig{
		Logger:         slog.New(slog.NewTextHandler(logs, nil)),
		HandlerTimeout: 30 * time.Millisecond,
	}, handler, &testIdentity{}, transport)

	transport.eventChan() <- testTextEvent("slow")

	select {
	case err := <-observed:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("handler context error = %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler timeout was not applied")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "timed out") {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want a timeout entry", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAck covers M2-2: the handler can answer a button press with a toast, and
// an unanswered event still gets an empty acknowledgement.
func TestAck(t *testing.T) {
	tests := []struct {
		name  string
		ack   bool
		text  string
		alert bool
		want  []ackCall
	}{
		{
			name: "handler acknowledges",
			ack:  true,
			text: "Подтверждено ✅",
			want: []ackCall{{token: "cb-1", text: "Подтверждено ✅"}},
		},
		{
			name:  "handler acknowledges with alert",
			ack:   true,
			text:  "Это напоминание не для вас",
			alert: true,
			want:  []ackCall{{token: "cb-1", text: "Это напоминание не для вас", alert: true}},
		},
		{
			name: "runtime acknowledges an unanswered event",
			want: []ackCall{{token: "cb-1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &testTransport{platform: PlatformTelegram}
			done := make(chan struct{})

			handler := func(ctx context.Context, botctx *Context, _ ResolvedEvent) error {
				defer close(done)
				if !tt.ack {
					return nil
				}
				return botctx.Ack(ctx, tt.text, tt.alert)
			}

			_, _ = startTestRuntime(t, RuntimeConfig{}, handler, &testIdentity{}, transport)

			transport.eventChan() <- Event{
				Kind:         EventAction,
				Platform:     PlatformTelegram,
				RemoteUserID: "42",
				ReceivedAt:   time.Now(),
				AckToken:     "cb-1",
				Action:       &ActionEvent{ActionID: "reminders.done"},
			}

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the handler did not run")
			}

			deadline := time.Now().Add(2 * time.Second)
			for {
				acks := transport.ackCalls()
				if len(acks) == len(tt.want) {
					for i := range tt.want {
						if acks[i] != tt.want[i] {
							t.Fatalf("ack[%d] = %+v, want %+v", i, acks[i], tt.want[i])
						}
					}
					return
				}
				if len(acks) > len(tt.want) {
					t.Fatalf("acks = %+v, want %+v", acks, tt.want)
				}
				if time.Now().After(deadline) {
					t.Fatalf("acks = %+v, want %+v", acks, tt.want)
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}

// TestAckWithoutToken checks that Matrix-like transports without an
// acknowledgement token need no special casing.
func TestAckWithoutToken(t *testing.T) {
	transport := &testTransport{platform: PlatformMatrix}
	done := make(chan struct{})
	handler := func(ctx context.Context, botctx *Context, _ ResolvedEvent) error {
		defer close(done)
		return botctx.Ack(ctx, "ignored", false)
	}
	_, _ = startTestRuntime(t, RuntimeConfig{}, handler, &testIdentity{}, transport)

	transport.eventChan() <- Event{
		Kind:         EventAction,
		Platform:     PlatformMatrix,
		RemoteUserID: "@u:x",
		ReceivedAt:   time.Now(),
		Action:       &ActionEvent{ActionID: "reminders.done"},
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not run")
	}
	time.Sleep(20 * time.Millisecond)
	if acks := transport.ackCalls(); len(acks) != 0 {
		t.Fatalf("acks = %+v, want none", acks)
	}
}

// TestResolveErrorIsLogged checks that a failing identity lookup is reported
// instead of silently swallowing the event.
func TestResolveErrorIsLogged(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}
	logs := &syncBuffer{}

	identity := &failingIdentity{err: errors.New("database is down")}
	_, _ = startTestRuntime(t, RuntimeConfig{Logger: slog.New(slog.NewTextHandler(logs, nil))},
		func(context.Context, *Context, ResolvedEvent) error {
			t.Error("the handler ran for an unresolved event")
			return nil
		}, identity, transport)

	transport.eventChan() <- testTextEvent("привет")

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "database is down") {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want the resolve error", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type failingIdentity struct{ err error }

func (i *failingIdentity) Bind(context.Context, Binding) error { return i.err }
func (i *failingIdentity) Resolve(context.Context, Platform, string) (PrincipalID, bool, error) {
	return "", false, i.err
}
func (i *failingIdentity) Bindings(context.Context, PrincipalID) ([]Binding, error) {
	return nil, i.err
}

// TestPrincipalRestriction checks that an interaction bound to one SSO account
// is not delivered to another.
func TestPrincipalRestriction(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}
	called := make(chan PrincipalID, 1)

	identity := &principalIdentity{principal: "principal-a"}
	handler := func(_ context.Context, _ *Context, event ResolvedEvent) error {
		called <- event.PrincipalID
		return nil
	}
	_, _ = startTestRuntime(t, RuntimeConfig{}, handler, identity, transport)

	send := func(restricted PrincipalID) {
		transport.eventChan() <- Event{
			Kind:         EventAction,
			Platform:     PlatformTelegram,
			RemoteUserID: "42",
			ReceivedAt:   time.Now(),
			AckToken:     "cb",
			Action:       &ActionEvent{ActionID: "reminders.done", PrincipalID: restricted},
		}
	}

	send("principal-b") // must be dropped
	select {
	case principal := <-called:
		t.Fatalf("handler ran for the wrong principal %q", principal)
	case <-time.After(100 * time.Millisecond):
	}

	send("principal-a") // must be delivered
	select {
	case principal := <-called:
		if principal != "principal-a" {
			t.Fatalf("principal = %q", principal)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not run for the right principal")
	}
}

type principalIdentity struct{ principal PrincipalID }

func (i *principalIdentity) Bind(context.Context, Binding) error { return nil }
func (i *principalIdentity) Resolve(context.Context, Platform, string) (PrincipalID, bool, error) {
	return i.principal, true, nil
}
func (i *principalIdentity) Bindings(context.Context, PrincipalID) ([]Binding, error) {
	return nil, nil
}

// TestCloseStopsProcessing checks clean shutdown: Close returns and no handler
// keeps running afterwards.
func TestCloseStopsProcessing(t *testing.T) {
	transport := &testTransport{platform: PlatformTelegram}
	started := make(chan struct{}, 1)

	handler := func(ctx context.Context, _ *Context, _ ResolvedEvent) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}

	cfg := RuntimeConfig{Logger: discardLogger(), HandlerTimeout: time.Minute}
	_ = transport.eventChan()
	runtime, err := NewRuntimeWithConfig(cfg, &testIdentity{}, handler, transport)
	if err != nil {
		t.Fatalf("NewRuntimeWithConfig: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	transport.eventChan() <- testTextEvent("long")

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not start")
	}

	closed := make(chan error, 1)
	go func() { closed <- runtime.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	cancel()
	// Close is idempotent.
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
