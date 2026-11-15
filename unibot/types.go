package unibot

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Platform string

const (
	PlatformTelegram Platform = "telegram"
	PlatformMatrix   Platform = "matrix"
)

type PrincipalID string

type Address struct {
	Platform       Platform
	RemoteUserID   string
	ConversationID string
}

type Binding struct {
	PrincipalID    PrincipalID
	Platform       Platform
	RemoteUserID   string
	ConversationID string
	VerifiedAt     time.Time
	Metadata       map[string]string
}

type ActionKind string

const (
	ActionSubmit  ActionKind = "submit"
	ActionURL     ActionKind = "url"
	ActionOpenApp ActionKind = "open_app"
)

type ActionStyle string

const (
	ActionStyleDefault ActionStyle = "default"
	ActionStylePrimary ActionStyle = "primary"
	ActionStyleSuccess ActionStyle = "success"
	ActionStyleDanger  ActionStyle = "danger"
)

type Action struct {
	ID    string
	Label string
	Icon  string
	Kind  ActionKind
	Style ActionStyle
	URL   string

	// Payload is an opaque string that comes back in ActionEvent when the user
	// presses the action. Features use it to carry identifiers (for example a
	// reminder and occurrence id) without encoding them in Action.ID. It is
	// bounded by MaxActionPayloadBytes.
	Payload string
	// PrincipalID optionally restricts the action to one SSO account. The
	// runtime drops the press for every other account, so a reminder cannot be
	// completed from somebody else's chat even if the button leaks.
	PrincipalID PrincipalID
	// TTL is how long the action stays valid. Zero means DefaultActionTTL.
	// Expired actions are rejected by the interaction store, so a stale button
	// never reaches the handler.
	TTL time.Duration
}

// DefaultActionTTL is how long a button stays active when Action.TTL is zero.
// It outlives the slowest reminder repeat (24 hours) and a process restart.
const DefaultActionTTL = 24 * time.Hour

// MaxActionPayloadBytes bounds Action.Payload. Telegram callback data is 64
// bytes, so the payload never travels through the messenger: it lives in the
// interaction store and is looked up by the callback token.
const MaxActionPayloadBytes = 512

type ChoiceOption struct {
	ID    string
	Label string
}

type Choice struct {
	ID            string
	Question      string
	Options       []ChoiceOption
	MaxSelections int
	Disclosed     bool
}

type Message struct {
	// Text is the plain-text body. Every transport can render it, so it stays
	// the fallback when HTML is not supported.
	Text string
	// HTML is an optional formatted variant of Text using the subset b, i,
	// code and a (@see ValidateHTML). It may be the only body of a message;
	// transports without HTML support fall back to Text or, when Text is
	// empty, to HTMLToText(HTML).
	HTML    string
	Actions [][]Action
	Choice  *Choice
}

func (m Message) Validate() error {
	if m.Text == "" && m.HTML == "" && m.Choice == nil {
		return errors.New("message must contain text, html or a choice")
	}
	if err := ValidateHTML(m.HTML); err != nil {
		return fmt.Errorf("invalid html: %w", err)
	}
	if len(m.Actions) > 0 && m.Choice != nil {
		return errors.New("starter renderer does not support actions and choice in the same logical message")
	}
	for _, row := range m.Actions {
		for _, action := range row {
			if action.ID == "" || action.Label == "" {
				return errors.New("every action needs id and label")
			}
			switch action.Kind {
			case ActionSubmit:
			case ActionURL, ActionOpenApp:
				if action.URL == "" {
					return errors.New("url/open_app action needs url")
				}
			default:
				return errors.New("unknown action kind")
			}
			if len(action.Payload) > MaxActionPayloadBytes {
				return fmt.Errorf("action %q payload is %d bytes, max %d", action.ID, len(action.Payload), MaxActionPayloadBytes)
			}
			if action.TTL < 0 {
				return fmt.Errorf("action %q has a negative ttl", action.ID)
			}
		}
	}
	if m.Choice != nil {
		if m.Choice.ID == "" || len(m.Choice.Options) < 2 {
			return errors.New("choice needs id and at least 2 options")
		}
		if m.Choice.MaxSelections < 0 {
			return errors.New("choice max selections cannot be negative")
		}
	}
	return nil
}

type SentMessage struct {
	Platform        Platform
	RemoteUserID    string
	ConversationID  string
	RemoteMessageID string
}

type EventKind string

const (
	EventMessage EventKind = "message"
	EventAction  EventKind = "action"
	EventChoice  EventKind = "choice"
)

type IncomingMessage struct {
	Text string
	// Command is set when the text starts with a slash command. Transports
	// fill it through ParseCommand so every messenger exposes /start, /help
	// and /cmd@botname args identically.
	Command *Command
}

// Command is a parsed slash command.
type Command struct {
	// Name is the command without the leading slash, lowercased, for example
	// "start" or "help".
	Name string
	// Addressee is the bot name of /cmd@botname, empty otherwise.
	Addressee string
	// Args is the text after the command name, trimmed.
	Args string
}

type ActionEvent struct {
	ActionID string
	// Payload is the opaque Action.Payload stored with the interaction.
	Payload string
	// PrincipalID, when set, restricts the action to this account: the runtime
	// drops the event for every other principal. It complements ExpectedUserID
	// (the messenger account) with the SSO identity, which survives relinking.
	PrincipalID PrincipalID
}

type ChoiceEvent struct {
	ChoiceID  string
	OptionIDs []string
	// PrincipalID is the choice counterpart of ActionEvent.PrincipalID.
	PrincipalID PrincipalID
}

type Event struct {
	Kind           EventKind
	Platform       Platform
	RemoteUserID   string
	ConversationID string
	RemoteEventID  string
	ReceivedAt     time.Time
	// AckToken is an opaque platform handle of an interactive event (a
	// Telegram callback query id). When it is set, the runtime acknowledges
	// the event after the handler returns unless the handler acknowledged it
	// itself with Context.Ack.
	AckToken string

	Message *IncomingMessage
	Action  *ActionEvent
	Choice  *ChoiceEvent
}

type ResolvedEvent struct {
	Event
	PrincipalID PrincipalID
	Linked      bool
}

type Capabilities struct {
	Text               bool
	Edit               bool
	Delete             bool
	Reactions          bool
	NativeActions      bool
	NativeChoice       bool
	EmbeddedWebApp     bool
	EndToEndEncryption bool
}

type Transport interface {
	Platform() Platform
	Capabilities() Capabilities
	Start(context.Context) error
	Close() error
	Events() <-chan Event
	Send(context.Context, Address, Message) (SentMessage, error)
	Edit(context.Context, SentMessage, Message) error
	Delete(context.Context, SentMessage) error
}

type IdentityStore interface {
	Bind(context.Context, Binding) error
	Resolve(context.Context, Platform, string) (PrincipalID, bool, error)
	Bindings(context.Context, PrincipalID) ([]Binding, error)
}

// Acknowledger is an optional Transport capability: it tells the messenger
// that an interactive event was received and may show a short toast or an
// alert dialog. Telegram implements it with answerCallbackQuery; Matrix is a
// no-op because a reaction needs no acknowledgement.
type Acknowledger interface {
	Ack(ctx context.Context, token, text string, alert bool) error
}

type Interaction struct {
	ActionID string
	ChoiceID string
	OptionID string
	// ExpectedUserID restricts the interaction to one messenger account.
	ExpectedUserID string
	// PrincipalID optionally restricts the interaction to one SSO account. It
	// is checked by the runtime after identity resolution.
	PrincipalID PrincipalID
	// Payload is the opaque Action.Payload delivered back in ActionEvent.
	Payload   string
	ExpiresAt time.Time
}

type InteractionStore interface {
	PutToken(context.Context, string, Interaction) error
	ResolveToken(context.Context, string) (Interaction, bool, error)
	PutMessageKey(context.Context, Platform, string, string, string, Interaction) error
	ResolveMessageKey(context.Context, Platform, string, string, string) (Interaction, bool, error)
}
