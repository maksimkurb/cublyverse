package unibot

import (
	"context"
	"errors"
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

type testTransport struct {
	platform Platform
	err      error
	mu       sync.Mutex
	sends    int
	events   chan Event
}

func (t *testTransport) Platform() Platform          { return t.platform }
func (t *testTransport) Capabilities() Capabilities  { return Capabilities{Text: true} }
func (t *testTransport) Start(context.Context) error { return nil }
func (t *testTransport) Close() error                { return nil }
func (t *testTransport) Events() <-chan Event {
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
