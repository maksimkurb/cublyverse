package unibot

import (
	"context"
	"errors"
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
}

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
	Text    string
	Actions [][]Action
	Choice  *Choice
}

func (m Message) Validate() error {
	if m.Text == "" && m.Choice == nil {
		return errors.New("message must contain text or a choice")
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
}

type ActionEvent struct {
	ActionID string
}

type ChoiceEvent struct {
	ChoiceID  string
	OptionIDs []string
}

type Event struct {
	Kind           EventKind
	Platform       Platform
	RemoteUserID   string
	ConversationID string
	RemoteEventID  string
	ReceivedAt     time.Time

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

type Interaction struct {
	ActionID       string
	ChoiceID       string
	OptionID       string
	ExpectedUserID string
	ExpiresAt      time.Time
}

type InteractionStore interface {
	PutToken(context.Context, string, Interaction) error
	ResolveToken(context.Context, string) (Interaction, bool, error)
	PutMessageKey(context.Context, Platform, string, string, string, Interaction) error
	ResolveMessageKey(context.Context, Platform, string, string, string) (Interaction, bool, error)
}
