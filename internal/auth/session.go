package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// Session is an authenticated session.
//
// The token is a 32-byte opaque secret. Only its sha256 is stored, so a leaked
// database contains no usable session. Token is filled in only right after
// creation; requests are authenticated by the hash.
type Session struct {
	ID          string
	PrincipalID string
	Kind        string
	ExpiresAt   time.Time
	// Token is the plaintext session secret, set only when the session was
	// just created.
	Token string
	// FromCookie reports that the request authenticated with the cookie rather
	// than the Authorization header. Mutating cookie requests need a JSON body
	// (CSRF protection on top of SameSite=Lax).
	FromCookie bool
}

// CreateSession issues a session for a principal. kind selects the transport:
// SessionKindBearer for the Telegram Mini App, SessionKindCookie for a browser.
func (s *Service) CreateSession(ctx context.Context, principalID, kind string) (Session, error) {
	userID, err := parseUUID(principalID)
	if err != nil {
		return Session{}, err
	}
	ttl := s.cfg.BearerSessionTTL
	switch kind {
	case SessionKindBearer:
	case SessionKindCookie:
		ttl = s.cfg.CookieSessionTTL
	default:
		return Session{}, fmt.Errorf("unknown session kind %q", kind)
	}

	token, err := newToken()
	if err != nil {
		return Session{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	row, err := s.st.Queries().CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(token),
		UserID:    userID,
		Kind:      kind,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return Session{
		ID:          formatUUID(row.ID),
		PrincipalID: formatUUID(row.UserID),
		Kind:        row.Kind,
		ExpiresAt:   row.ExpiresAt,
		Token:       token,
	}, nil
}

// ResolveSession authenticates an HTTP request. A Bearer token (Mini App) wins
// over the cookie; each session kind is only accepted through its own
// transport, so a cookie value is never usable as a Bearer token.
func (s *Service) ResolveSession(ctx context.Context, r *http.Request) (Session, bool) {
	if token := bearerToken(r); token != "" {
		return s.sessionFromToken(ctx, token, false)
	}
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		return s.sessionFromToken(ctx, cookie.Value, true)
	}
	return Session{}, false
}

func (s *Service) sessionFromToken(ctx context.Context, token string, fromCookie bool) (Session, bool) {
	row, err := s.st.Queries().GetSessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false
	}
	if err != nil {
		s.logger.WarnContext(ctx, "resolve session failed", "error", err)
		return Session{}, false
	}
	wantKind := SessionKindBearer
	if fromCookie {
		wantKind = SessionKindCookie
	}
	if row.Kind != wantKind {
		return Session{}, false
	}
	// last_used_at is informational; writing it on every request would turn a
	// read into a write, so it is refreshed at most once per interval.
	if !row.LastUsedAt.Valid || time.Since(row.LastUsedAt.Time) > sessionTouchInterval {
		if err := s.st.Queries().TouchSession(ctx, row.ID); err != nil {
			s.logger.WarnContext(ctx, "touch session failed", "error", err)
		}
	}
	return Session{
		ID:          formatUUID(row.ID),
		PrincipalID: formatUUID(row.UserID),
		Kind:        row.Kind,
		ExpiresAt:   row.ExpiresAt,
		FromCookie:  fromCookie,
	}, true
}

// RevokeSession invalidates a session immediately. The row stays until the next
// cleanup, but every lookup rejects it.
func (s *Service) RevokeSession(ctx context.Context, sessionID string) error {
	id, err := parseUUID(sessionID)
	if err != nil {
		return err
	}
	if err := s.st.Queries().RevokeSession(ctx, id); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// SessionCookie builds the browser session cookie. Secure is set whenever the
// public URL is HTTPS; SameSite=Lax plus HttpOnly keep the token away from
// scripts and from cross-site POSTs.
func (s *Service) SessionCookie(token string, expiresAt time.Time) *http.Cookie {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
	}
	if maxAge := int(time.Until(expiresAt).Seconds()); maxAge > 0 {
		cookie.MaxAge = maxAge
	}
	return cookie
}

// ClearedSessionCookie serialises an already expired cookie. The API returns it
// in the Set-Cookie header of POST /auth/logout.
func (s *Service) ClearedSessionCookie() string {
	cookie := s.SessionCookie("", time.Unix(0, 0).UTC())
	cookie.MaxAge = -1
	return cookie.String()
}

// bearerToken extracts the token of an "Authorization: Bearer …" header.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
