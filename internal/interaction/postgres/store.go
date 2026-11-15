package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// defaultTTL is used when an interaction has no explicit expiry.
const defaultTTL = unibot.DefaultActionTTL

// Store is the PostgreSQL interaction registry.
type Store struct {
	st *store.Store
}

// New returns a store backed by st.
func New(st *store.Store) *Store { return &Store{st: st} }

var _ unibot.InteractionStore = (*Store)(nil)

// PutToken registers an action or choice option under an opaque token. Only the
// sha256 of the token is stored: the token itself is a bearer secret that
// travels through the messenger.
func (s *Store) PutToken(ctx context.Context, token string, interaction unibot.Interaction) error {
	if token == "" {
		return errors.New("interaction token is required")
	}
	principalID, err := parsePrincipal(interaction.PrincipalID)
	if err != nil {
		return err
	}
	expiresAt := expiry(interaction)
	sum := sha256.Sum256([]byte(token))
	if _, err := s.st.Queries().CreateInteractionByToken(ctx, db.CreateInteractionByTokenParams{
		TokenHash:      sum[:],
		ActionID:       interaction.ActionID,
		ChoiceID:       interaction.ChoiceID,
		OptionID:       interaction.OptionID,
		ExpectedUserID: interaction.ExpectedUserID,
		PrincipalID:    principalID,
		Payload:        interaction.Payload,
		ExpiresAt:      expiresAt,
	}); err != nil {
		return fmt.Errorf("store interaction token: %w", err)
	}
	return nil
}

// ResolveToken looks an interaction up by its token. Expired rows are not
// returned, so a stale button does nothing.
func (s *Store) ResolveToken(ctx context.Context, token string) (unibot.Interaction, bool, error) {
	if token == "" {
		return unibot.Interaction{}, false, nil
	}
	sum := sha256.Sum256([]byte(token))
	row, err := s.st.Queries().GetInteractionByTokenHash(ctx, sum[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return unibot.Interaction{}, false, nil
	}
	if err != nil {
		return unibot.Interaction{}, false, fmt.Errorf("resolve interaction token: %w", err)
	}
	return interactionFromRow(row), true, nil
}

// PutMessageKey registers an interaction under the message it belongs to.
func (s *Store) PutMessageKey(ctx context.Context, platform unibot.Platform, conversationID, messageID, key string, interaction unibot.Interaction) error {
	if platform == "" || conversationID == "" || messageID == "" || key == "" {
		return errors.New("platform, conversation, message and key are required")
	}
	principalID, err := parsePrincipal(interaction.PrincipalID)
	if err != nil {
		return err
	}
	expiresAt := expiry(interaction)
	if _, err := s.st.Queries().UpsertInteractionByMessageKey(ctx, db.UpsertInteractionByMessageKeyParams{
		Platform:       strPtr(string(platform)),
		ConversationID: strPtr(conversationID),
		MessageID:      strPtr(messageID),
		ActionKey:      strPtr(key),
		ActionID:       interaction.ActionID,
		ChoiceID:       interaction.ChoiceID,
		OptionID:       interaction.OptionID,
		ExpectedUserID: interaction.ExpectedUserID,
		PrincipalID:    principalID,
		Payload:        interaction.Payload,
		ExpiresAt:      expiresAt,
	}); err != nil {
		return fmt.Errorf("store interaction message key: %w", err)
	}
	return nil
}

// ResolveMessageKey looks an interaction up by the message it belongs to.
func (s *Store) ResolveMessageKey(ctx context.Context, platform unibot.Platform, conversationID, messageID, key string) (unibot.Interaction, bool, error) {
	if platform == "" || conversationID == "" || messageID == "" || key == "" {
		return unibot.Interaction{}, false, nil
	}
	row, err := s.st.Queries().GetInteractionByMessageKey(ctx, db.GetInteractionByMessageKeyParams{
		Platform:       strPtr(string(platform)),
		ConversationID: strPtr(conversationID),
		MessageID:      strPtr(messageID),
		ActionKey:      strPtr(key),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return unibot.Interaction{}, false, nil
	}
	if err != nil {
		return unibot.Interaction{}, false, fmt.Errorf("resolve interaction message key: %w", err)
	}
	return interactionFromRow(row), true, nil
}

// Cleanup deletes expired interactions and reports how many rows went away.
func (s *Store) Cleanup(ctx context.Context) (int64, error) {
	deleted, err := s.st.Queries().DeleteExpiredInteractions(ctx)
	if err != nil {
		return 0, fmt.Errorf("delete expired interactions: %w", err)
	}
	return deleted, nil
}

// expiry returns when an interaction stops being valid. An interaction that is
// already expired is stored as-is: the lookup query filters on expiry, so it
// behaves exactly like a stale row.
func expiry(interaction unibot.Interaction) time.Time {
	if interaction.ExpiresAt.IsZero() {
		return time.Now().UTC().Add(defaultTTL)
	}
	return interaction.ExpiresAt.UTC()
}

// parsePrincipal turns the optional principal restriction into a SQL value.
// An empty principal means "no restriction" and is stored as NULL.
func parsePrincipal(principal unibot.PrincipalID) (pgtype.UUID, error) {
	if principal == "" {
		return pgtype.UUID{}, nil
	}
	var parsed pgtype.UUID
	if err := parsed.Scan(string(principal)); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid principal id %q: %w", principal, err)
	}
	if !parsed.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid principal id %q", principal)
	}
	return parsed, nil
}

func interactionFromRow(row db.Interaction) unibot.Interaction {
	return unibot.Interaction{
		ActionID:       row.ActionID,
		ChoiceID:       row.ChoiceID,
		OptionID:       row.OptionID,
		ExpectedUserID: row.ExpectedUserID,
		PrincipalID:    formatPrincipal(row.PrincipalID),
		Payload:        row.Payload,
		ExpiresAt:      row.ExpiresAt,
	}
}

func formatPrincipal(principal pgtype.UUID) unibot.PrincipalID {
	if !principal.Valid {
		return ""
	}
	b := principal.Bytes
	return unibot.PrincipalID(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

func strPtr(value string) *string { return &value }
