package unibot

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Handler func(context.Context, *Context, ResolvedEvent) error

type Runtime struct {
	identity IdentityStore
	handler  Handler

	mu         sync.RWMutex
	transports map[Platform]Transport
}

func NewRuntime(identity IdentityStore, handler Handler, transports ...Transport) (*Runtime, error) {
	if identity == nil {
		return nil, errors.New("identity store is required")
	}
	if handler == nil {
		return nil, errors.New("handler is required")
	}

	r := &Runtime{
		identity:   identity,
		handler:    handler,
		transports: make(map[Platform]Transport, len(transports)),
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

func (r *Runtime) Start(ctx context.Context) error {
	r.mu.RLock()
	transports := make([]Transport, 0, len(r.transports))
	for _, transport := range r.transports {
		transports = append(transports, transport)
	}
	r.mu.RUnlock()

	for _, transport := range transports {
		if err := transport.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", transport.Platform(), err)
		}
		go r.consume(ctx, transport)
	}
	return nil
}

func (r *Runtime) consume(ctx context.Context, transport Transport) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-transport.Events():
			if !ok {
				return
			}
			principal, linked, err := r.identity.Resolve(ctx, evt.Platform, evt.RemoteUserID)
			if err != nil {
				continue
			}
			resolved := ResolvedEvent{Event: evt, PrincipalID: principal, Linked: linked}
			_ = r.handler(ctx, &Context{runtime: r, event: resolved}, resolved)
		}
	}
}

func (r *Runtime) Close() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out error
	for _, transport := range r.transports {
		out = errors.Join(out, transport.Close())
	}
	return out
}

type Context struct {
	runtime *Runtime
	event   ResolvedEvent
}

func (c *Context) Event() ResolvedEvent { return c.event }

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
	r.mu.RLock()
	transport := r.transports[address.Platform]
	r.mu.RUnlock()
	if transport == nil {
		return SentMessage{}, fmt.Errorf("no transport for %s", address.Platform)
	}
	return transport.Send(ctx, address, message)
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
