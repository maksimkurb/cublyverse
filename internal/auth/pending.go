package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Status values of auth_pending, mirrored by the CHECK constraint.
const (
	statusPendingOIDC          = "pending_oidc"
	statusAwaitingConfirmation = "awaiting_confirmation"
	statusCompleted            = "completed"
)

// pendingSecrets are the freshly minted secrets of a sign-in flow. Only their
// state token is handed to the browser; the nonce and the PKCE verifier stay in
// PostgreSQL.
type pendingSecrets struct {
	State     string
	Nonce     string
	Verifier  string
	ExpiresAt time.Time
}

type pendingInput struct {
	flow           string
	platform       string
	remoteUserID   string
	conversationID string
	returnTo       string
}

// createPending stores a new pending sign-in flow and returns its state token.
func (s *Service) createPending(ctx context.Context, in pendingInput) (pendingSecrets, error) {
	state, err := newToken()
	if err != nil {
		return pendingSecrets{}, err
	}
	nonce, err := newToken()
	if err != nil {
		return pendingSecrets{}, err
	}
	verifier, err := newToken()
	if err != nil {
		return pendingSecrets{}, err
	}
	expiresAt := time.Now().UTC().Add(s.cfg.PendingTTL)
	if _, err := s.st.Queries().CreatePendingAuth(ctx, db.CreatePendingAuthParams{
		StateHash:      hashToken(state),
		Status:         statusPendingOIDC,
		Flow:           in.flow,
		Nonce:          nonce,
		Verifier:       verifier,
		Platform:       in.platform,
		RemoteUserID:   in.remoteUserID,
		ConversationID: in.conversationID,
		ReturnTo:       in.returnTo,
		ExpiresAt:      expiresAt,
	}); err != nil {
		return pendingSecrets{}, fmt.Errorf("create pending sign-in: %w", err)
	}
	return pendingSecrets{State: state, Nonce: nonce, Verifier: verifier, ExpiresAt: expiresAt}, nil
}

// loadPending looks a flow up by the state token from the URL.
func (s *Service) loadPending(ctx context.Context, state string) (db.AuthPending, error) {
	if state == "" {
		return db.AuthPending{}, ErrPendingUnusable
	}
	row, err := s.st.Queries().GetPendingAuthByStateHash(ctx, hashToken(state))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.AuthPending{}, ErrPendingUnusable
	}
	if err != nil {
		return db.AuthPending{}, fmt.Errorf("load pending sign-in: %w", err)
	}
	if !row.ExpiresAt.After(time.Now()) {
		return db.AuthPending{}, ErrPendingUnusable
	}
	return row, nil
}

// BeginMessengerBinding starts the flow that links a messenger account to an
// SSO account. The returned URL is what the "Войти" button opens; the binding
// is only created after the user confirms it.
func (s *Service) BeginMessengerBinding(ctx context.Context, platform unibot.Platform, remoteUserID, conversationID string) (string, error) {
	switch platform {
	case unibot.PlatformTelegram, unibot.PlatformMatrix:
	default:
		return "", fmt.Errorf("unsupported platform %q", platform)
	}
	if remoteUserID == "" {
		return "", errors.New("remote user id is required")
	}
	pending, err := s.createPending(ctx, pendingInput{
		flow:           FlowBind,
		platform:       string(platform),
		remoteUserID:   remoteUserID,
		conversationID: conversationID,
	})
	if err != nil {
		return "", err
	}
	return s.loginURL(pending.State), nil
}

// BeginBrowserLogin starts a plain browser sign-in and returns the provider
// redirect together with the pending flow.
func (s *Service) BeginBrowserLogin(ctx context.Context, returnTo string) (string, pendingSecrets, error) {
	if s.oidc == nil {
		return "", pendingSecrets{}, ErrOIDCDisabled
	}
	validated, ok := SafeReturnTo(returnTo)
	if !ok {
		return "", pendingSecrets{}, fmt.Errorf("%w: %q", ErrInvalidReturnTo, returnTo)
	}
	pending, err := s.createPending(ctx, pendingInput{flow: FlowBrowser, returnTo: validated})
	if err != nil {
		return "", pendingSecrets{}, err
	}
	return s.oidc.authCodeURL(pending.State, pending.Nonce, pending.Verifier), pending, nil
}

// loginURL is the page that starts the OIDC dance for a pending flow.
func (s *Service) loginURL(state string) string {
	return s.absoluteURL("/auth/login?state=" + url.QueryEscape(state))
}

// ConfirmBinding creates the messenger binding after the user pressed the
// confirmation button. The state and the confirmation token are single use: the
// first POST wins, later ones get ErrPendingUnusable.
func (s *Service) ConfirmBinding(ctx context.Context, state, token string) (unibot.Binding, error) {
	row, err := s.loadPending(ctx, state)
	if err != nil {
		return unibot.Binding{}, err
	}
	if row.Status != statusAwaitingConfirmation {
		return unibot.Binding{}, ErrPendingUnusable
	}
	if len(row.ConfirmTokenHash) == 0 || !tokensMatch(row.ConfirmTokenHash, hashToken(token)) {
		return unibot.Binding{}, ErrConfirmTokenInvalid
	}
	return s.completeBinding(ctx, row)
}

// completeBinding claims the pending flow and writes the binding in one
// transaction, so a failure cannot leave a consumed state without a binding.
func (s *Service) completeBinding(ctx context.Context, row db.AuthPending) (unibot.Binding, error) {
	var binding unibot.Binding
	err := s.st.InTx(ctx, func(q *db.Queries) error {
		claimed, err := q.CompletePendingAuth(ctx, row.StateHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPendingUnusable
		}
		if err != nil {
			return fmt.Errorf("complete pending sign-in: %w", err)
		}
		if !claimed.UserID.Valid {
			return errors.New("pending sign-in has no user")
		}
		created, err := q.UpsertBinding(ctx, db.UpsertBindingParams{
			UserID:         claimed.UserID,
			Platform:       claimed.Platform,
			RemoteUserID:   claimed.RemoteUserID,
			ConversationID: claimed.ConversationID,
			VerifiedAt:     time.Now().UTC(),
			Metadata:       []byte("{}"),
		})
		if err != nil {
			return fmt.Errorf("bind messenger account: %w", err)
		}
		binding = unibot.Binding{
			PrincipalID:    unibot.PrincipalID(formatUUID(created.UserID)),
			Platform:       unibot.Platform(created.Platform),
			RemoteUserID:   created.RemoteUserID,
			ConversationID: created.ConversationID,
			VerifiedAt:     created.VerifiedAt,
		}
		return nil
	})
	return binding, err
}

// deletePending forgets a completed flow.
func (s *Service) deletePending(ctx context.Context, stateHash []byte) error {
	if err := s.st.Queries().DeletePendingAuth(ctx, stateHash); err != nil {
		return fmt.Errorf("delete pending sign-in: %w", err)
	}
	return nil
}

// Cleanup deletes expired sign-in flows and dead sessions. It backs the
// background job of the same service.
func (s *Service) Cleanup(ctx context.Context) error {
	pending, err := s.st.Queries().DeleteExpiredPendingAuth(ctx)
	if err != nil {
		return fmt.Errorf("delete expired pending sign-ins: %w", err)
	}
	sessions, err := s.st.Queries().DeleteStaleSessions(ctx)
	if err != nil {
		return fmt.Errorf("delete stale sessions: %w", err)
	}
	if pending > 0 || sessions > 0 {
		s.logger.Info("auth cleanup finished",
			"pending_deleted", pending,
			"sessions_deleted", sessions,
		)
	}
	return nil
}
