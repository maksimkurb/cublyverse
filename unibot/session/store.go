package session

import (
	"context"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Store persists application session state by canonical SSO principal.
// Messenger-specific IDs must never be used as the session key.
type Store[T any] interface {
	Load(context.Context, unibot.PrincipalID) (T, bool, error)
	Save(context.Context, unibot.PrincipalID, T) error
	Delete(context.Context, unibot.PrincipalID) error
}
