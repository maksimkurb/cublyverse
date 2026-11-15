package unibot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Handler answers one resolved messenger event. It runs on the runtime's
// handler pool, so it may block: the runtime bounds concurrency and applies a
// timeout. Returning an error is fine — it is logged, not retried.
type Handler func(context.Context, *Context, ResolvedEvent) error

const (
	// DefaultHandlerTimeout bounds one handler run.
	DefaultHandlerTimeout = 30 * time.Second
	// DefaultMaxConcurrentHandlers is the number of users handled in parallel.
	// Events of one user are always handled one at a time, in order.
	DefaultMaxConcurrentHandlers = 32
	// DefaultMaxPendingEvents bounds events waiting for their user's previous
	// event to finish.
	DefaultMaxPendingEvents = 1024
	// defaultEventQueueSize bounds the hand-off between a transport and the
	// scheduler.
	defaultEventQueueSize = 256
	// ackTimeout bounds the acknowledgement of an interactive event.
	ackTimeout = 5 * time.Second
	// shutdownGrace is added to the handler timeout when waiting for workers in
	// Close, so a stuck handler cannot hang the process forever.
	shutdownGrace = 5 * time.Second
)

// RuntimeConfig tunes the runtime. The zero value is valid: every field falls
// back to a safe default.
type RuntimeConfig struct {
	// Logger records handler errors, panics, timeouts and acknowledgement
	// failures. Nil means slog.Default().
	Logger *slog.Logger
	// HandlerTimeout bounds one handler run. Zero means DefaultHandlerTimeout.
	HandlerTimeout time.Duration
	// MaxConcurrentHandlers is how many users are handled in parallel.
	MaxConcurrentHandlers int
	// MaxPendingEvents bounds buffered events. Zero means DefaultMaxPendingEvents.
	MaxPendingEvents int
	// QueueSize bounds the channel between the transports and the scheduler.
	QueueSize int
}

func (c RuntimeConfig) withDefaults() RuntimeConfig {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.HandlerTimeout <= 0 {
		c.HandlerTimeout = DefaultHandlerTimeout
	}
	if c.MaxConcurrentHandlers <= 0 {
		c.MaxConcurrentHandlers = DefaultMaxConcurrentHandlers
	}
	if c.MaxPendingEvents <= 0 {
		c.MaxPendingEvents = DefaultMaxPendingEvents
	}
	if c.QueueSize <= 0 {
		c.QueueSize = defaultEventQueueSize
	}
	return c
}

// Runtime wires transports to one handler.
//
// Events of the same remote user are handled in arrival order, one at a time;
// different users run concurrently. The work is bounded: a fixed number of
// concurrent handlers, a bounded pending queue and a bounded hand-off channel.
// Handler errors and panics are logged, never fatal.
type Runtime struct {
	identity IdentityStore
	handler  Handler
	cfg      RuntimeConfig

	mu         sync.RWMutex
	transports map[Platform]Transport
	cancel     context.CancelFunc

	// incoming carries events from the per-transport consumers to the
	// scheduler; completions reports a finished user back to it.
	incoming    chan Event
	completions chan string

	// dispatch tracks the consumers and the scheduler; handlers tracks the
	// handler goroutines. Close waits for dispatch first, so no new handler is
	// started while handlers are being awaited.
	dispatch sync.WaitGroup
	handlers sync.WaitGroup

	startOnce sync.Once
	closeOnce sync.Once
	closeErr  error
}

// NewRuntime builds a runtime with default settings.
func NewRuntime(identity IdentityStore, handler Handler, transports ...Transport) (*Runtime, error) {
	return NewRuntimeWithConfig(RuntimeConfig{}, identity, handler, transports...)
}

// NewRuntimeWithConfig builds a runtime with explicit settings.
func NewRuntimeWithConfig(cfg RuntimeConfig, identity IdentityStore, handler Handler, transports ...Transport) (*Runtime, error) {
	if identity == nil {
		return nil, errors.New("identity store is required")
	}
	if handler == nil {
		return nil, errors.New("handler is required")
	}
	cfg = cfg.withDefaults()

	r := &Runtime{
		identity:    identity,
		handler:     handler,
		cfg:         cfg,
		transports:  make(map[Platform]Transport, len(transports)),
		incoming:    make(chan Event, cfg.QueueSize),
		completions: make(chan string, cfg.MaxConcurrentHandlers),
	}
	for _, transport := range transports {
		if transport == nil {
			continue
		}
		if _, exists := r.transports[transport.Platform()]; exists {
			return nil, fmt.Errorf("transport %q registered twice", transport.Platform())
		}
		r.transports[transport.Platform()] = transport
	}
	return r, nil
}

// Logger returns the runtime logger.
func (r *Runtime) Logger() *slog.Logger { return r.cfg.Logger }

func (r *Runtime) AddTransport(transport Transport) error {
	if transport == nil {
		return errors.New("nil transport")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.transports[transport.Platform()]; exists {
		return fmt.Errorf("transport %q already registered", transport.Platform())
	}
	r.transports[transport.Platform()] = transport
	return nil
}

// Start starts every transport, the consumer of its events and the scheduler.
// It returns as soon as they are running; processing stops when ctx is
// cancelled or Close is called.
func (r *Runtime) Start(ctx context.Context) error {
	var startErr error
	r.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)

		r.mu.Lock()
		r.cancel = cancel
		transports := make([]Transport, 0, len(r.transports))
		for _, transport := range r.transports {
			transports = append(transports, transport)
		}
		r.mu.Unlock()

		for _, transport := range transports {
			if err := transport.Start(runCtx); err != nil {
				startErr = fmt.Errorf("start %s: %w", transport.Platform(), err)
				cancel()
				return
			}
			r.dispatch.Add(1)
			go func(t Transport) {
				defer r.dispatch.Done()
				r.consume(runCtx, t)
			}(transport)
		}

		r.dispatch.Add(1)
		go func() {
			defer r.dispatch.Done()
			r.schedule(runCtx)
		}()
	})
	return startErr
}

// consume moves events of one transport into the scheduler queue, applying
// backpressure when the queue is full.
func (r *Runtime) consume(ctx context.Context, transport Transport) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-transport.Events():
			if !ok {
				return
			}
			select {
			case r.incoming <- evt:
			case <-ctx.Done():
				return
			}
		}
	}
}

// schedule keeps per-user order: an event of a user whose previous event is
// still running waits in pending, while other users are dispatched right away.
// Everything is bounded by MaxConcurrentHandlers and MaxPendingEvents, so a
// flood of users cannot create unbounded goroutines or memory.
func (r *Runtime) schedule(ctx context.Context) {
	pending := make(map[string][]Event)
	active := make(map[string]struct{})
	// ready is a FIFO of users with pending events; queued mirrors it for O(1)
	// duplicate checks.
	ready := make([]string, 0, r.cfg.MaxConcurrentHandlers)
	queued := make(map[string]struct{})
	pendingCount := 0

	for {
		// Dispatch queued events while there is a free handler slot. A user is
		// only ever dispatched from here when no handler of that user is
		// running, which is what keeps the per-user order; ready itself is a
		// FIFO so no waiting user starves.
		for len(active) < r.cfg.MaxConcurrentHandlers && len(ready) > 0 {
			key := ready[0]
			ready = ready[1:]
			delete(queued, key)

			queue := pending[key]
			if len(queue) == 0 {
				continue
			}
			next := queue[0]
			if len(queue) == 1 {
				delete(pending, key)
			} else {
				pending[key] = queue[1:]
			}
			pendingCount--
			active[key] = struct{}{}
			r.startHandler(ctx, key, next)
		}

		// Stop reading new events while the pending buffer is full: the
		// transport channels then apply backpressure on the messenger.
		var incoming <-chan Event
		if pendingCount < r.cfg.MaxPendingEvents {
			incoming = r.incoming
		}

		select {
		case <-ctx.Done():
			return
		case key := <-r.completions:
			delete(active, key)
			if len(pending[key]) == 0 {
				continue
			}
			// The user's next event waits for this slot; queue it for dispatch.
			if _, exists := queued[key]; !exists {
				queued[key] = struct{}{}
				ready = append(ready, key)
			}
		case evt := <-incoming:
			key := eventKey(evt)
			_, busy := active[key]
			if busy || len(active) >= r.cfg.MaxConcurrentHandlers {
				pending[key] = append(pending[key], evt)
				pendingCount++
				// A busy user is released by its completion; an idle user only
				// waits for a free slot and has to be queued now.
				if !busy {
					if _, exists := queued[key]; !exists {
						queued[key] = struct{}{}
						ready = append(ready, key)
					}
				}
				continue
			}
			active[key] = struct{}{}
			r.startHandler(ctx, key, evt)
		}
	}
}

// startHandler runs one event and reports its user back to the scheduler.
func (r *Runtime) startHandler(ctx context.Context, key string, evt Event) {
	r.handlers.Add(1)
	go func() {
		defer r.handlers.Done()
		r.handleEvent(ctx, evt)
		select {
		case r.completions <- key:
		case <-ctx.Done():
		}
	}()
}

// handleEvent resolves the messenger account to a principal and invokes the
// handler. The acknowledgement of an interactive event is always sent, even
// when resolution fails or the handler skips the event.
func (r *Runtime) handleEvent(ctx context.Context, evt Event) {
	botctx := &Context{runtime: r, event: ResolvedEvent{Event: evt}}
	defer r.finishAck(ctx, botctx)

	if ctx.Err() != nil {
		// Shutting down: do not start new work or log a false failure.
		return
	}

	principal, linked, err := r.identity.Resolve(ctx, evt.Platform, evt.RemoteUserID)
	if err != nil {
		r.cfg.Logger.ErrorContext(ctx, "resolve messenger identity failed",
			slog.String("platform", string(evt.Platform)),
			slog.String("event_kind", string(evt.Kind)),
			slog.String("error", err.Error()),
		)
		return
	}
	botctx.event.PrincipalID = principal
	botctx.event.Linked = linked

	if restriction := eventPrincipalRestriction(evt); restriction != "" && restriction != principal {
		r.cfg.Logger.WarnContext(ctx, "dropping event addressed to another principal",
			slog.String("platform", string(evt.Platform)),
			slog.String("event_kind", string(evt.Kind)),
		)
		return
	}

	r.invoke(ctx, botctx)
}

// invoke runs the handler with a timeout and recovers from panics so one broken
// event cannot take down the process.
func (r *Runtime) invoke(ctx context.Context, botctx *Context) {
	evt := botctx.event
	defer func() {
		if recovered := recover(); recovered != nil {
			r.cfg.Logger.ErrorContext(ctx, "bot handler panicked",
				slog.String("platform", string(evt.Platform)),
				slog.String("event_kind", string(evt.Kind)),
				slog.Any("panic", recovered),
				slog.String("stack", string(debug.Stack())),
			)
		}
	}()

	handlerCtx, cancel := context.WithTimeout(ctx, r.cfg.HandlerTimeout)
	defer cancel()

	err := r.handler(handlerCtx, botctx, evt)
	switch {
	case err == nil:
		return
	case ctx.Err() != nil:
		// The process is shutting down; the handler was interrupted on purpose.
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(handlerCtx.Err(), context.DeadlineExceeded):
		r.cfg.Logger.ErrorContext(ctx, "bot handler timed out",
			slog.String("platform", string(evt.Platform)),
			slog.String("event_kind", string(evt.Kind)),
			slog.Duration("timeout", r.cfg.HandlerTimeout),
		)
	default:
		r.cfg.Logger.ErrorContext(ctx, "bot handler failed",
			slog.String("platform", string(evt.Platform)),
			slog.String("event_kind", string(evt.Kind)),
			slog.String("error", err.Error()),
		)
	}
}

// finishAck sends the empty acknowledgement the messenger expects when the
// handler did not acknowledge the event itself.
func (r *Runtime) finishAck(ctx context.Context, botctx *Context) {
	evt := botctx.event.Event
	if evt.AckToken == "" || botctx.acknowledged() {
		return
	}
	// The event is already answered when the process stops: acknowledge with a
	// context that is not cancelled by the shutdown.
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	if err := r.ack(ackCtx, evt.Platform, evt.AckToken, "", false); err != nil {
		r.cfg.Logger.WarnContext(ackCtx, "acknowledge messenger event failed",
			slog.String("platform", string(evt.Platform)),
			slog.String("error", err.Error()),
		)
	}
}

// ack sends the acknowledgement when the transport supports it.
func (r *Runtime) ack(ctx context.Context, platform Platform, token, text string, alert bool) error {
	r.mu.RLock()
	transport := r.transports[platform]
	r.mu.RUnlock()
	if transport == nil {
		return fmt.Errorf("no transport for %s", platform)
	}
	acknowledger, ok := transport.(Acknowledger)
	if !ok {
		return nil
	}
	return acknowledger.Ack(ctx, token, text, alert)
}

// Close stops processing and closes every transport. It is safe to call more
// than once and also when Start was never called.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		cancel := r.cancel
		r.mu.Unlock()
		if cancel != nil {
			cancel()
		}

		r.mu.RLock()
		transports := make([]Transport, 0, len(r.transports))
		for _, transport := range r.transports {
			transports = append(transports, transport)
		}
		r.mu.RUnlock()

		var out error
		for _, transport := range transports {
			out = errors.Join(out, transport.Close())
		}

		// Consumers and the scheduler must stop before the handlers are
		// awaited: afterwards no goroutine can add to the handler group.
		grace := 2*r.cfg.HandlerTimeout + shutdownGrace
		if !waitGroupWithin(&r.dispatch, grace) {
			r.cfg.Logger.Warn("messenger dispatch did not stop in time")
		}
		if !waitGroupWithin(&r.handlers, grace) {
			r.cfg.Logger.Warn("messenger handlers did not stop in time")
		}
		r.closeErr = out
	})
	return r.closeErr
}

// waitGroupWithin waits for wg and reports false on timeout. A timed-out wait
// leaves one goroutine behind, which is the lesser evil compared to a process
// that never exits.
func waitGroupWithin(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// eventKey identifies the ordering domain of an event: one remote user on one
// platform. Events of the same key are handled strictly one after another.
func eventKey(evt Event) string {
	return string(evt.Platform) + "\x00" + evt.RemoteUserID
}

// eventPrincipalRestriction returns the optional principal restriction carried
// by an action or choice interaction.
func eventPrincipalRestriction(evt Event) PrincipalID {
	switch {
	case evt.Action != nil:
		return evt.Action.PrincipalID
	case evt.Choice != nil:
		return evt.Choice.PrincipalID
	default:
		return ""
	}
}

// Context is the per-event hand-off to the handler.
type Context struct {
	runtime *Runtime
	event   ResolvedEvent
	ackSent atomic.Bool
}

func (c *Context) Event() ResolvedEvent { return c.event }

// Ack confirms an interactive event (a pressed button). text is shown as a
// short notification by messengers that support it; alert makes it a modal
// dialog. A handler that does not call Ack still gets an empty
// acknowledgement from the runtime, so a button never keeps spinning.
//
// Ack is a no-op for events without an AckToken and for transports without
// acknowledgement support (Matrix). Calling it twice sends nothing twice.
func (c *Context) Ack(ctx context.Context, text string, alert bool) error {
	token := c.event.AckToken
	if token == "" {
		return nil
	}
	if !c.ackSent.CompareAndSwap(false, true) {
		return nil
	}
	if err := c.runtime.ack(ctx, c.event.Platform, token, text, alert); err != nil {
		// Let the runtime retry with an empty acknowledgement.
		c.ackSent.Store(false)
		return fmt.Errorf("acknowledge event: %w", err)
	}
	return nil
}

func (c *Context) acknowledged() bool { return c.ackSent.Load() }

func (c *Context) Reply(ctx context.Context, message Message) (SentMessage, error) {
	return c.runtime.SendToAddress(ctx, Address{
		Platform:       c.event.Platform,
		RemoteUserID:   c.event.RemoteUserID,
		ConversationID: c.event.ConversationID,
	}, message)
}

func (r *Runtime) SendToAddress(ctx context.Context, address Address, message Message) (SentMessage, error) {
	if err := message.Validate(); err != nil {
		return SentMessage{}, err
	}
	transport, err := r.transport(address.Platform)
	if err != nil {
		return SentMessage{}, err
	}
	return transport.Send(ctx, address, message)
}

// Edit replaces an already sent message in place. Features use it to turn a
// reminder into its «✅ Выполнено» form without sending a second message.
func (r *Runtime) Edit(ctx context.Context, sent SentMessage, message Message) error {
	if err := message.Validate(); err != nil {
		return err
	}
	transport, err := r.transport(sent.Platform)
	if err != nil {
		return err
	}
	return transport.Edit(ctx, sent, message)
}

// Delete removes an already sent message. Deleting is always best effort: the
// message may already be gone, and a failure must not break the caller's state
// change.
func (r *Runtime) Delete(ctx context.Context, sent SentMessage) error {
	transport, err := r.transport(sent.Platform)
	if err != nil {
		return err
	}
	return transport.Delete(ctx, sent)
}

// transport returns the registered transport of one platform.
func (r *Runtime) transport(platform Platform) (Transport, error) {
	r.mu.RLock()
	transport := r.transports[platform]
	r.mu.RUnlock()
	if transport == nil {
		return nil, fmt.Errorf("no transport for %s", platform)
	}
	return transport, nil
}

type DeliveryOptions struct {
	Preferred []Platform
	Fanout    bool
}

func (r *Runtime) SendToPrincipal(ctx context.Context, principal PrincipalID, message Message, opts DeliveryOptions) ([]SentMessage, error) {
	if err := message.Validate(); err != nil {
		return nil, err
	}
	bindings, err := r.identity.Bindings(ctx, principal)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, errors.New("principal has no bindings")
	}

	ordered := orderBindings(bindings, opts.Preferred)
	var sent []SentMessage
	var errs error
	for _, binding := range ordered {
		msg, err := r.SendToAddress(ctx, Address{
			Platform:       binding.Platform,
			RemoteUserID:   binding.RemoteUserID,
			ConversationID: binding.ConversationID,
		}, message)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s: %w", binding.Platform, err))
			continue
		}
		sent = append(sent, msg)
		if !opts.Fanout {
			return sent, nil
		}
	}
	if len(sent) > 0 {
		return sent, errs
	}
	if errs == nil {
		errs = errors.New("no usable binding")
	}
	return nil, errs
}

func orderBindings(bindings []Binding, preferred []Platform) []Binding {
	if len(preferred) == 0 {
		return append([]Binding(nil), bindings...)
	}
	weight := make(map[Platform]int, len(preferred))
	for i, platform := range preferred {
		weight[platform] = i
	}
	out := append([]Binding(nil), bindings...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			wi, iok := weight[out[i].Platform]
			wj, jok := weight[out[j].Platform]
			if (!iok && jok) || (iok && jok && wj < wi) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
