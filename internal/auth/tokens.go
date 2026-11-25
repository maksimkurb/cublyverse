package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// Personal access tokens authenticate clients that cannot hold a session: MCP
// servers, scripts and CI. A token is `cv_pat_` followed by 43 URL-safe base64
// characters of 32 random bytes; only its sha256 is stored.
const (
	// APITokenPrefix marks a personal access token in the Authorization header.
	// It is also the reason a PAT can never be mistaken for a session token.
	APITokenPrefix = "cv_pat_"
	// apiTokenVisibleChars is how many characters after the marker are shown in
	// the WebApp so a user can recognise the row.
	apiTokenVisibleChars = 8
	// MaxAPITokenNameRunes bounds the human readable name of a token.
	MaxAPITokenNameRunes = 80
	// MinAPITokenExpiryDays and MaxAPITokenExpiryDays bound the optional
	// lifetime chosen at creation.
	MinAPITokenExpiryDays = 1
	MaxAPITokenExpiryDays = 365
	// apiTokenTouchInterval throttles last_used_at writes: without it every MCP
	// call would turn an authentication read into a write.
	apiTokenTouchInterval = time.Minute
	// patFailureBurst is how many failed token lookups one remote IP may make
	// before the basic rate limit kicks in, and patFailureRefill is how fast the
	// bucket recovers.
	patFailureBurst   = 20
	patFailureRefill  = 2 * time.Second
	patRateLimitSweep = 10 * time.Minute
)

// Scopes a personal access token may be granted. Adding a scope means adding a
// registry entry and a check in the adapter that needs it; the database stores
// plain strings, so no migration is involved.
const (
	// ScopeProfileRead grants reading the user's own profile.
	ScopeProfileRead = "profile:read"
	// ScopeRemindersRead grants reading reminders.
	ScopeRemindersRead = "reminders:read"
	// ScopeRemindersWrite grants creating, changing, completing and deleting
	// reminders.
	ScopeRemindersWrite = "reminders:write"
	// ScopeCraftRead grants reading the catalogue, stock, projects and wish
	// list of a workspace.
	ScopeCraftRead = "craft:read"
	// ScopeCraftWrite grants changing them.
	ScopeCraftWrite = "craft:write"
)

// Scope describes one grantable permission.
type Scope struct {
	// Name is the stable identifier stored in api_tokens.scopes.
	Name string
	// Description is user-facing Russian text for the WebApp and the API.
	Description string
}

// Scopes is the registry of every scope a token may carry. Order is the order
// the WebApp shows them in.
var Scopes = []Scope{
	{Name: ScopeProfileRead, Description: "Чтение профиля"},
	{Name: ScopeRemindersRead, Description: "Чтение напоминаний"},
	{Name: ScopeRemindersWrite, Description: "Изменение напоминаний"},
	{Name: ScopeCraftRead, Description: "Чтение запасов и каталога («Бусинки»)"},
	{Name: ScopeCraftWrite, Description: "Изменение запасов, каталога и закупок («Бусинки»)"},
}

// ErrAPITokenNotFound means the token does not exist or belongs to another
// user. Both cases are reported the same way.
var ErrAPITokenNotFound = errors.New("api token not found")

// APITokenValidationError is a rejected token input. The message is user-facing
// and Russian, so the HTTP layer can show it as is.
type APITokenValidationError struct {
	// Message explains what is wrong.
	Message string
}

func (e *APITokenValidationError) Error() string { return e.Message }

// APIToken is a stored personal access token. The plaintext secret is never
// part of it: it exists only in the response of the creation call.
type APIToken struct {
	// ID is the token UUID.
	ID string
	// Name is the user-facing label.
	Name string
	// Prefix is the visible head of the token (the characters after the
	// marker), used to tell rows apart.
	Prefix string
	// Scopes are the granted permissions.
	Scopes []string
	// ExpiresAt is the optional expiry.
	ExpiresAt *time.Time
	// LastUsedAt is refreshed at most once per minute while the token is used.
	LastUsedAt *time.Time
	// RevokedAt is set when the user revoked the token.
	RevokedAt *time.Time
	// CreatedAt is when the token was issued.
	CreatedAt time.Time
}

// IsAPIToken reports whether a bearer value is a personal access token rather
// than a session token. The marker is what keeps the two transports apart.
func IsAPIToken(token string) bool { return strings.HasPrefix(token, APITokenPrefix) }

// ValidScope reports whether name is a known scope.
func ValidScope(name string) bool {
	return slices.ContainsFunc(Scopes, func(scope Scope) bool { return scope.Name == name })
}

// ScopeNames returns the registry in a stable order.
func ScopeNames() []string {
	out := make([]string, 0, len(Scopes))
	for _, scope := range Scopes {
		out = append(out, scope.Name)
	}
	return out
}

// normalizeScopes validates, de-duplicates and canonicalises the requested
// scopes. The order follows the registry, so the stored value is deterministic.
func normalizeScopes(requested []string) ([]string, error) {
	seen := make(map[string]struct{}, len(requested))
	for _, scope := range requested {
		scope = strings.TrimSpace(scope)
		if !ValidScope(scope) {
			return nil, &APITokenValidationError{Message: fmt.Sprintf("Неизвестная область доступа %q", scope)}
		}
		seen[scope] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, &APITokenValidationError{Message: "Выберите хотя бы одну область доступа"}
	}
	out := make([]string, 0, len(seen))
	for _, scope := range Scopes {
		if _, ok := seen[scope.Name]; ok {
			out = append(out, scope.Name)
		}
	}
	return out, nil
}

// CreateAPIToken issues a personal access token. The plaintext is returned once
// and never stored: the database keeps only its sha256.
func (s *Service) CreateAPIToken(
	ctx context.Context,
	principalID, name string,
	scopes []string,
	expiresInDays *int,
) (APIToken, string, error) {
	userID, err := parseUUID(principalID)
	if err != nil {
		return APIToken{}, "", err
	}

	name = strings.TrimSpace(name)
	if length := utf8.RuneCountInString(name); length < 1 || length > MaxAPITokenNameRunes {
		return APIToken{}, "", &APITokenValidationError{
			Message: fmt.Sprintf("Название токена должно быть от 1 до %d символов", MaxAPITokenNameRunes),
		}
	}

	normalized, err := normalizeScopes(scopes)
	if err != nil {
		return APIToken{}, "", err
	}

	var expiresAt *time.Time
	if expiresInDays != nil {
		days := *expiresInDays
		if days < MinAPITokenExpiryDays || days > MaxAPITokenExpiryDays {
			return APIToken{}, "", &APITokenValidationError{
				Message: fmt.Sprintf(
					"Срок действия должен быть от %d до %d дней",
					MinAPITokenExpiryDays, MaxAPITokenExpiryDays,
				),
			}
		}
		expiry := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &expiry
	}

	secret, err := newToken()
	if err != nil {
		return APIToken{}, "", err
	}
	token := APITokenPrefix + secret
	prefix := token[len(APITokenPrefix):]
	prefix = prefix[:apiTokenVisibleChars]

	row, err := s.st.Queries().CreateAPIToken(ctx, db.CreateAPITokenParams{
		UserID:      userID,
		Name:        name,
		TokenPrefix: prefix,
		TokenHash:   hashToken(token),
		Scopes:      normalized,
		ExpiresAt:   timestampOrZero(expiresAt),
	})
	if err != nil {
		return APIToken{}, "", fmt.Errorf("create api token: %w", err)
	}
	return apiTokenFromRow(row), token, nil
}

// ListAPITokens returns every token of the principal, including revoked and
// expired ones: the WebApp shows their state.
func (s *Service) ListAPITokens(ctx context.Context, principalID string) ([]APIToken, error) {
	userID, err := parseUUID(principalID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListAPITokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	out := make([]APIToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiTokenFromRow(row))
	}
	return out, nil
}

// RevokeAPIToken invalidates a token immediately. Revoking an already revoked
// token is a no-op, so the two-step confirmation in the WebApp is safe to
// retry; a foreign or unknown id is ErrAPITokenNotFound.
func (s *Service) RevokeAPIToken(ctx context.Context, principalID, id string) error {
	userID, err := parseUUID(principalID)
	if err != nil {
		return err
	}
	tokenID, err := parseUUID(id)
	if err != nil {
		return ErrAPITokenNotFound
	}
	affected, err := s.st.Queries().RevokeAPIToken(ctx, db.RevokeAPITokenParams{
		ID:     tokenID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if affected == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

// ResolveAPIToken authenticates a personal access token. Unknown, expired and
// revoked tokens are indistinguishable: all of them answer false.
//
// The request feeds a basic in-memory rate limit of failed lookups per remote
// address: a client that sprays tokens burns its bucket instead of the
// database. A successful lookup empties the bucket again.
func (s *Service) ResolveAPIToken(ctx context.Context, token string, r *http.Request) (Session, bool) {
	if !IsAPIToken(token) || len(token) <= len(APITokenPrefix) {
		return Session{}, false
	}
	ip := remoteIP(r)
	if !s.patLimiter.allow(ip) {
		s.logger.WarnContext(ctx, "api token lookup rate limited", "remote_addr", ip)
		return Session{}, false
	}

	// The attempt itself was consumed by allow, so a failed lookup needs no
	// further bookkeeping; only a success refills the bucket.
	row, err := s.st.Queries().GetAPITokenByHash(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false
	}
	if err != nil {
		s.logger.WarnContext(ctx, "resolve api token failed", "error", err)
		return Session{}, false
	}
	s.patLimiter.succeed(ip)

	if !row.LastUsedAt.Valid || time.Since(row.LastUsedAt.Time) > apiTokenTouchInterval {
		if err := s.st.Queries().TouchAPIToken(ctx, row.ID); err != nil {
			s.logger.WarnContext(ctx, "touch api token failed", "error", err)
		}
	}

	return Session{
		ID:          formatUUID(row.ID),
		PrincipalID: formatUUID(row.UserID),
		Kind:        SessionKindToken,
		ExpiresAt:   expiresAtOrZero(row.ExpiresAt),
		Scopes:      append([]string(nil), row.Scopes...),
		APITokenID:  formatUUID(row.ID),
	}, true
}

// remoteIP returns the client address without the port, for rate limiting. A
// missing or unparsable address buckets together under "unknown".
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if r.RemoteAddr != "" {
			return r.RemoteAddr
		}
		return "unknown"
	}
	return host
}

func apiTokenFromRow(row db.ApiToken) APIToken {
	return APIToken{
		ID:         formatUUID(row.ID),
		Name:       row.Name,
		Prefix:     row.TokenPrefix,
		Scopes:     append([]string(nil), row.Scopes...),
		ExpiresAt:  timePtrOrZero(row.ExpiresAt),
		LastUsedAt: timePtrOrZero(row.LastUsedAt),
		RevokedAt:  timePtrOrZero(row.RevokedAt),
		CreatedAt:  row.CreatedAt.UTC(),
	}
}

// timestampOrZero converts an optional instant to a nullable column value.
func timestampOrZero(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

// timePtrOrZero converts a nullable column value to an optional instant. A NULL
// column becomes nil, which is how the API reports "never expires" and "never
// used".
func timePtrOrZero(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	utc := value.Time.UTC()
	return &utc
}

// expiresAtOrZero is the scalar form used by Session.
func expiresAtOrZero(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

// patRateLimiter is a token bucket per remote IP for failed token lookups. It
// is intentionally simple: a map, a mutex and lazy refill, swept when it grows.
// It protects the database from token spraying, not the API from a determined
// attacker; that is what a reverse proxy is for.
type patRateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*patBucket
	lastSweep time.Time
}

type patBucket struct {
	tokens float64
	last   time.Time
}

func newPATRateLimiter() *patRateLimiter {
	return &patRateLimiter{buckets: make(map[string]*patBucket), lastSweep: time.Now()}
}

// allow consumes one attempt of the bucket. An empty bucket rejects the request
// before it reaches PostgreSQL.
func (l *patRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked()

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &patBucket{tokens: patFailureBurst, last: time.Now()}
		l.buckets[key] = bucket
	}
	l.refill(bucket)
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// succeed resets the bucket of a client that proved it holds a valid token.
func (l *patRateLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

func (l *patRateLimiter) refill(bucket *patBucket) {
	elapsed := time.Since(bucket.last)
	if elapsed <= 0 {
		return
	}
	bucket.tokens += elapsed.Seconds() / patFailureRefill.Seconds()
	if bucket.tokens > patFailureBurst {
		bucket.tokens = patFailureBurst
	}
	bucket.last = time.Now()
}

// sweepLocked drops idle buckets so the map cannot grow without bound.
func (l *patRateLimiter) sweepLocked() {
	now := time.Now()
	if now.Sub(l.lastSweep) < patRateLimitSweep {
		return
	}
	for key, bucket := range l.buckets {
		if now.Sub(bucket.last) > patRateLimitSweep {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}
