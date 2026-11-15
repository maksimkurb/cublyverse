// Package bot routes messenger commands and actions to feature handlers.
//
// The router is the single entry point of the messenger side: adapters
// (unibot) produce resolved events, the router decides which feature handler
// runs. Features register their commands and action prefixes through the
// Feature interface, so `internal/app` only has to wire the registry once.
package bot

import (
	"context"
	"log/slog"
	"strings"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Handler answers one resolved messenger event.
type Handler func(context.Context, *unibot.Context, unibot.ResolvedEvent) error

// CommandInfo describes a registered command. It feeds /help and the Telegram
// command menu (setMyCommands).
type CommandInfo struct {
	// Name is the command without the leading slash, for example "start".
	Name string
	// Description is the one-line Russian text shown next to the command.
	Description string
}

// Router dispatches resolved messenger events to the handlers registered by
// features and by the core.
//
// Registration happens once during startup, before the runtime starts, so the
// router itself is safe to use from concurrent handler goroutines without
// locking.
type Router struct {
	logger    *slog.Logger
	links     LoginLinks
	webAppURL string

	commands        []CommandInfo
	commandHandlers map[string]Handler
	actions         []actionEntry
	unlinked        Handler
	fallback        Handler
}

type actionEntry struct {
	prefix  string
	handler Handler
}

// Option customizes the router at construction.
type Option func(*Router)

// WithLogger sets the router logger.
func WithLogger(logger *slog.Logger) Option {
	return func(r *Router) {
		if logger != nil {
			r.logger = logger
		}
	}
}

// WithLoginLinks sets the service that starts the messenger binding flow.
func WithLoginLinks(links LoginLinks) Option {
	return func(r *Router) { r.links = links }
}

// WithWebAppURL sets the absolute WebApp URL used by «Открыть приложение».
func WithWebAppURL(url string) Option {
	return func(r *Router) { r.webAppURL = strings.TrimSpace(url) }
}

// NewRouter returns a router with the core commands (/start and /help) and the
// default «Войти» handler for accounts without a binding.
func NewRouter(opts ...Option) *Router {
	r := &Router{
		logger:          slog.Default(),
		commandHandlers: make(map[string]Handler),
	}
	for _, opt := range opts {
		opt(r)
	}
	r.unlinked = r.handleUnlinked
	r.fallback = r.handleFallback
	r.registerCoreCommands()
	return r
}

// Command registers or replaces the handler of /name. Description is shown by
// /help and published as the Telegram command menu.
func (r *Router) Command(name, description string, handler Handler) {
	name = normalizeCommandName(name)
	if name == "" || handler == nil {
		return
	}
	if _, exists := r.commandHandlers[name]; exists {
		for i := range r.commands {
			if r.commands[i].Name == name {
				r.commands[i].Description = description
			}
		}
	} else {
		r.commands = append(r.commands, CommandInfo{Name: name, Description: description})
	}
	r.commandHandlers[name] = handler
}

// Action registers the handler for actions whose id starts with prefix, for
// example "reminders." for "reminders.done". The longest registered prefix
// wins, so a feature can register both "reminders." and "reminders.done".
func (r *Router) Action(prefix string, handler Handler) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || handler == nil {
		return
	}
	r.actions = append(r.actions, actionEntry{prefix: prefix, handler: handler})
}

// Unlinked replaces the handler of events from an account without a binding.
// The default sends the «Войти» button.
func (r *Router) Unlinked(handler Handler) {
	if handler != nil {
		r.unlinked = handler
	}
}

// Fallback replaces the handler of events that matched no command or action.
func (r *Router) Fallback(handler Handler) {
	if handler != nil {
		r.fallback = handler
	}
}

// Commands returns the registered commands in registration order.
func (r *Router) Commands() []CommandInfo {
	out := make([]CommandInfo, len(r.commands))
	copy(out, r.commands)
	return out
}

// Handle is the unibot.Handler entry point of the router.
func (r *Router) Handle(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent) error {
	if !event.Linked {
		return r.unlinked(ctx, botctx, event)
	}

	switch event.Kind {
	case unibot.EventMessage:
		if event.Message != nil && event.Message.Command != nil {
			if handler, ok := r.commandHandlers[event.Message.Command.Name]; ok {
				return handler(ctx, botctx, event)
			}
			return r.replyUnknownCommand(ctx, botctx)
		}
	case unibot.EventAction:
		if event.Action != nil {
			if handler, ok := r.actionHandler(event.Action.ActionID); ok {
				return handler(ctx, botctx, event)
			}
			return r.replyStaleAction(ctx, botctx)
		}
	}

	return r.fallback(ctx, botctx, event)
}

// actionHandler returns the handler of the longest matching action prefix.
func (r *Router) actionHandler(actionID string) (Handler, bool) {
	var best Handler
	bestLen := -1
	for _, entry := range r.actions {
		if !strings.HasPrefix(actionID, entry.prefix) {
			continue
		}
		if len(entry.prefix) > bestLen {
			best, bestLen = entry.handler, len(entry.prefix)
		}
	}
	return best, bestLen >= 0
}

// normalizeCommandName accepts both "help" and "/help".
func normalizeCommandName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "/")))
}
