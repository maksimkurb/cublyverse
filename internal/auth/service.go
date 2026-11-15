// Package auth owns identity: SSO users, messenger bindings, pending sign-in
// flows and sessions.
//
// Three entry points share this package: the HTTP API (sessions, /me), the
// browser pages under /auth, and the messenger runtime through the
// unibot.IdentityStore implementation. The service is the only place that
// decides whether a messenger account may be attached to an SSO user, so the
// rule cannot drift between them.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Default lifetimes of sign-in artefacts. They are deliberately constants: the
// values are part of the security model and are documented in docs/API.md.
const (
	// DefaultPendingTTL bounds how long a login link stays usable.
	DefaultPendingTTL = 10 * time.Minute
	// DefaultCookieSessionTTL is the lifetime of a browser session cookie.
	DefaultCookieSessionTTL = 30 * 24 * time.Hour
	// DefaultBearerSessionTTL is the lifetime of a Telegram Mini App session.
	DefaultBearerSessionTTL = 24 * time.Hour
	// DefaultInitDataMaxAge is the accepted age of a Telegram initData payload.
	DefaultInitDataMaxAge = 24 * time.Hour
	// DefaultCleanupInterval is how often expired rows are deleted.
	DefaultCleanupInterval = 5 * time.Minute
	// sessionTouchInterval throttles last_used_at writes so a busy client does
	// not cause a write per request.
	sessionTouchInterval = time.Minute
)

// SessionCookieName is the browser session cookie. It is HttpOnly, so the
// WebApp never sees the token; the browser attaches it automatically.
const SessionCookieName = "cv_session"

// Session kinds stored in the sessions table.
const (
	SessionKindBearer = "bearer"
	SessionKindCookie = "cookie"
	// SessionKindToken marks a request authenticated with a personal access
	// token. It is never stored in the sessions table: PATs live in api_tokens.
	SessionKindToken = "api_token"
)

// Sign-in flow kinds stored in auth_pending.
const (
	FlowBind    = "bind"
	FlowBrowser = "browser"
)

// Errors returned to the HTTP layer; handlers translate them into responses.
var (
	// ErrTelegramDisabled means TELEGRAM_BOT_TOKEN is not configured.
	ErrTelegramDisabled = errors.New("telegram sign-in is not configured")
	// ErrOIDCDisabled means the OIDC settings are missing.
	ErrOIDCDisabled = errors.New("OIDC is not configured")
	// ErrInvalidInitData covers a forged signature, a malformed payload and an
	// expired auth_date; the caller must not learn which one it was.
	ErrInvalidInitData = errors.New("invalid telegram initData")
	// ErrIdentityMismatch means the initData belongs to another Telegram user
	// than the pending binding.
	ErrIdentityMismatch = errors.New("initData does not match the pending binding")
	// ErrPendingUnusable means the state is unknown, expired or already used.
	ErrPendingUnusable = errors.New("sign-in link expired or already used")
	// ErrConfirmTokenInvalid means the confirmation token is wrong.
	ErrConfirmTokenInvalid = errors.New("confirmation token is invalid")
	// ErrInvalidReturnTo means the post-login redirect target is not a
	// same-site relative path.
	ErrInvalidReturnTo = errors.New("return_to must be a same-site relative path")
)

// OIDCConfig holds the OpenID Connect client settings.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is the absolute /auth/callback URL registered at the
	// provider. It is derived from PublicBaseURL when empty.
	RedirectURL string
}

// Config is the sign-in and session configuration.
type Config struct {
	// PublicBaseURL is used for absolute links and to decide whether cookies
	// must carry the Secure flag.
	PublicBaseURL string
	OIDC          OIDCConfig
	// TelegramBotToken validates Mini App initData.
	TelegramBotToken string

	// PendingTTL, CookieSessionTTL, BearerSessionTTL and InitDataMaxAge
	// default to the Default* constants. Tests shorten them.
	PendingTTL       time.Duration
	CookieSessionTTL time.Duration
	BearerSessionTTL time.Duration
	InitDataMaxAge   time.Duration

	// CleanupInterval defaults to DefaultCleanupInterval.
	CleanupInterval time.Duration
}

func (c Config) withDefaults() Config {
	if c.PendingTTL <= 0 {
		c.PendingTTL = DefaultPendingTTL
	}
	if c.CookieSessionTTL <= 0 {
		c.CookieSessionTTL = DefaultCookieSessionTTL
	}
	if c.BearerSessionTTL <= 0 {
		c.BearerSessionTTL = DefaultBearerSessionTTL
	}
	if c.InitDataMaxAge <= 0 {
		c.InitDataMaxAge = DefaultInitDataMaxAge
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = DefaultCleanupInterval
	}
	c.PublicBaseURL = strings.TrimRight(strings.TrimSpace(c.PublicBaseURL), "/")
	return c
}

// Service is the identity, binding and session service.
type Service struct {
	st     *store.Store
	cfg    Config
	logger *slog.Logger
	oidc   *oidcClient

	// patLimiter throttles failed personal access token lookups per remote IP.
	patLimiter *patRateLimiter

	// OnLinked is called after a messenger binding was confirmed. The
	// application uses it to thank the user in the chat.
	OnLinked func(ctx context.Context, binding unibot.Binding)
}

// Service is the PostgreSQL identity store of the messenger runtime.
var _ unibot.IdentityStore = (*Service)(nil)

// New builds the service. When the OIDC issuer is set, provider discovery
// happens here so a wrong configuration fails at start instead of in the middle
// of a login.
func New(ctx context.Context, st *store.Store, cfg Config, logger *slog.Logger) (*Service, error) {
	if st == nil {
		return nil, errors.New("store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	cfg = cfg.withDefaults()

	s := &Service{st: st, cfg: cfg, logger: logger, patLimiter: newPATRateLimiter()}

	if cfg.OIDC.Issuer != "" {
		if cfg.OIDC.ClientID == "" {
			return nil, errors.New("OIDC_CLIENT_ID is required when OIDC_ISSUER is set")
		}
		if cfg.OIDC.RedirectURL == "" {
			if cfg.PublicBaseURL == "" {
				return nil, errors.New("OIDC_REDIRECT_URL or PUBLIC_BASE_URL is required when OIDC_ISSUER is set")
			}
			cfg.OIDC.RedirectURL = cfg.PublicBaseURL + "/auth/callback"
			s.cfg.OIDC.RedirectURL = cfg.OIDC.RedirectURL
		}
		client, err := newOIDCClient(ctx, cfg.OIDC)
		if err != nil {
			return nil, err
		}
		s.oidc = client
	}

	return s, nil
}

// OIDCEnabled reports whether SSO sign-in is configured.
func (s *Service) OIDCEnabled() bool { return s.oidc != nil }

// User is the SSO account as exposed to the application.
type User struct {
	// ID is the canonical principal id (users.id, formatted as a UUID).
	ID          string
	Email       *string
	DisplayName *string
	Timezone    string
	CreatedAt   time.Time
}

// Binding is a verified messenger account.
type Binding struct {
	Platform       string
	RemoteUserID   string
	ConversationID string
	VerifiedAt     time.Time
}

// Profile is the authenticated user together with the linked messenger
// accounts. It backs GET /me.
type Profile struct {
	User     User
	Bindings []Binding
}

// OIDCIdentity is the verified claim set of an SSO login.
type OIDCIdentity struct {
	Issuer      string
	Subject     string
	Email       *string
	DisplayName *string
}

// UpsertFromOIDC creates or updates the user row of an SSO account. The
// (issuer, subject) pair is the identity key; the e-mail and the display name
// follow the provider.
func (s *Service) UpsertFromOIDC(ctx context.Context, identity OIDCIdentity) (User, error) {
	if identity.Issuer == "" || identity.Subject == "" {
		return User{}, errors.New("issuer and subject are required")
	}
	row, err := s.st.Queries().UpsertUserFromOIDC(ctx, db.UpsertUserFromOIDCParams{
		OidcIssuer:  identity.Issuer,
		OidcSubject: identity.Subject,
		Email:       identity.Email,
		DisplayName: identity.DisplayName,
	})
	if err != nil {
		return User{}, fmt.Errorf("upsert user from OIDC: %w", err)
	}
	return userFromRow(row), nil
}

// Profile loads the user and the messenger bindings of a principal.
func (s *Service) Profile(ctx context.Context, principalID string) (Profile, error) {
	userID, err := parseUUID(principalID)
	if err != nil {
		return Profile{}, err
	}
	row, err := s.st.Queries().GetUserByID(ctx, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("load user: %w", err)
	}
	bindings, err := s.MessengerBindings(ctx, principalID)
	if err != nil {
		return Profile{}, err
	}
	return Profile{User: userFromRow(row), Bindings: bindings}, nil
}

// UpdateTimezone stores the user's IANA time zone.
func (s *Service) UpdateTimezone(ctx context.Context, principalID, timezone string) (Profile, error) {
	if err := ValidateTimezone(timezone); err != nil {
		return Profile{}, err
	}
	userID, err := parseUUID(principalID)
	if err != nil {
		return Profile{}, err
	}
	if _, err := s.st.Queries().UpdateUserTimezone(ctx, db.UpdateUserTimezoneParams{
		ID:       userID,
		Timezone: timezone,
	}); err != nil {
		return Profile{}, fmt.Errorf("update timezone: %w", err)
	}
	return s.Profile(ctx, principalID)
}

// ValidateTimezone accepts IANA time zone names only. "Local" is rejected
// because it depends on the server environment, not on the user.
func ValidateTimezone(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("timezone must not be empty")
	}
	if name == "Local" {
		return errors.New("timezone must be an IANA name")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return fmt.Errorf("unknown timezone %q", name)
	}
	return nil
}

// Bind implements unibot.IdentityStore. Re-binding a messenger account to
// another user moves the binding; it never duplicates it.
func (s *Service) Bind(ctx context.Context, binding unibot.Binding) error {
	if binding.Platform == "" || binding.RemoteUserID == "" {
		return errors.New("platform and remote user id are required")
	}
	userID, err := parseUUID(string(binding.PrincipalID))
	if err != nil {
		return err
	}
	verifiedAt := binding.VerifiedAt
	if verifiedAt.IsZero() {
		verifiedAt = time.Now().UTC()
	}
	metadata, err := marshalMetadata(binding.Metadata)
	if err != nil {
		return err
	}
	if _, err := s.st.Queries().UpsertBinding(ctx, db.UpsertBindingParams{
		UserID:         userID,
		Platform:       string(binding.Platform),
		RemoteUserID:   binding.RemoteUserID,
		ConversationID: binding.ConversationID,
		VerifiedAt:     verifiedAt,
		Metadata:       metadata,
	}); err != nil {
		return fmt.Errorf("bind messenger account: %w", err)
	}
	return nil
}

// Resolve implements unibot.IdentityStore.
func (s *Service) Resolve(ctx context.Context, platform unibot.Platform, remoteUserID string) (unibot.PrincipalID, bool, error) {
	if platform == "" || remoteUserID == "" {
		return "", false, nil
	}
	row, err := s.st.Queries().GetBinding(ctx, db.GetBindingParams{
		Platform:     string(platform),
		RemoteUserID: remoteUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve messenger binding: %w", err)
	}
	return unibot.PrincipalID(formatUUID(row.UserID)), true, nil
}

// Bindings implements unibot.IdentityStore.
func (s *Service) Bindings(ctx context.Context, principal unibot.PrincipalID) ([]unibot.Binding, error) {
	items, err := s.MessengerBindings(ctx, string(principal))
	if err != nil {
		return nil, err
	}
	out := make([]unibot.Binding, 0, len(items))
	for _, item := range items {
		out = append(out, unibot.Binding{
			PrincipalID:    principal,
			Platform:       unibot.Platform(item.Platform),
			RemoteUserID:   item.RemoteUserID,
			ConversationID: item.ConversationID,
			VerifiedAt:     item.VerifiedAt,
		})
	}
	return out, nil
}

// MessengerBindings returns the verified messenger accounts of a principal.
func (s *Service) MessengerBindings(ctx context.Context, principalID string) ([]Binding, error) {
	userID, err := parseUUID(principalID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListBindingsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list messenger bindings: %w", err)
	}
	out := make([]Binding, 0, len(rows))
	for _, row := range rows {
		out = append(out, Binding{
			Platform:       row.Platform,
			RemoteUserID:   row.RemoteUserID,
			ConversationID: row.ConversationID,
			VerifiedAt:     row.VerifiedAt,
		})
	}
	return out, nil
}

func userFromRow(row db.User) User {
	return User{
		ID:          formatUUID(row.ID),
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Timezone:    row.Timezone,
		CreatedAt:   row.CreatedAt,
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// cookieSecure reports whether session cookies need the Secure attribute. The
// public URL is the only source of truth: a production deployment serves HTTPS.
func (s *Service) cookieSecure() bool {
	return strings.HasPrefix(strings.ToLower(s.cfg.PublicBaseURL), "https://")
}

// absoluteURL turns a site-relative path into an absolute URL when
// PUBLIC_BASE_URL is configured. Messenger buttons need absolute URLs.
func (s *Service) absoluteURL(path string) string {
	if s.cfg.PublicBaseURL == "" {
		return path
	}
	return s.cfg.PublicBaseURL + path
}
