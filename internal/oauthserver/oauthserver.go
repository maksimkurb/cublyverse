// Package oauthserver is the OAuth 2.1 authorization server of Cublyverse for
// MCP clients.
//
// The MCP authorization specification makes an MCP server an OAuth resource
// server and expects clients to register themselves with the authorization
// server on the fly. Common identity providers (Authelia among them) do not
// support dynamic registration, so Cublyverse issues MCP tokens itself and only
// borrows the *user* from the existing SSO session:
//
//	client ─ /oauth/authorize ─▶ sign-in with the SSO session ─▶ consent page
//	       ◀─ code ────────────────────────────────────────────────┘
//	client ─ /oauth/token (code + PKCE verifier) ─▶ access + refresh token
//	client ─ /mcp (Bearer access token)
//
// Supported: authorization code with mandatory PKCE (S256), refresh token
// rotation with reuse detection, dynamic client registration (RFC 7591),
// client ID metadata documents (CIMD), resource indicators (RFC 8707), protected
// resource metadata (RFC 9728), authorization server metadata (RFC 8414) and
// revocation (RFC 7009). Only public clients exist: the secret-less client is
// protected by PKCE and exact redirect URI matching.
package oauthserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// Token formats. The prefixes keep the three bearer kinds (session, personal
// access token, OAuth token) apart without a database lookup.
const (
	AccessTokenPrefix  = "cv_oat_"
	RefreshTokenPrefix = "cv_ort_"
)

// Lifetimes.
const (
	codeTTL         = 5 * time.Minute
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	// cimdRefresh is how long a fetched client metadata document is trusted.
	cimdRefresh = 24 * time.Hour
)

// Paths of the endpoints, relative to the public base URL.
const (
	PathAuthorize = "/oauth/authorize"
	PathToken     = "/oauth/token"
	PathRegister  = "/oauth/register"
	PathRevoke    = "/oauth/revoke"
)

// Identity resolves the browser session of a request.
type Identity interface {
	ResolveSession(ctx context.Context, r *http.Request) (auth.Session, bool)
}

// Config configures the server.
type Config struct {
	// Issuer is the public base URL without a trailing slash, for example
	// https://cublyverse.example. It is the OAuth issuer identifier and the
	// root of every resource URL, so it must be the URL clients really use.
	Issuer string
	// Logger receives errors. Nil uses the default logger.
	Logger *slog.Logger
	// Clock overrides time.Now in tests.
	Clock func() time.Time
	// HTTPClient fetches client metadata documents. Nil uses a client that
	// refuses private addresses and redirects.
	HTTPClient *http.Client
}

// Service is the authorization server.
type Service struct {
	cfg      Config
	st       *store.Store
	identity Identity
	http     *http.Client

	mu       sync.Mutex
	registry map[string]*bucket // DCR rate limit per remote address
}

// New returns the service.
func New(cfg Config, st *store.Store, identity Identity) *Service {
	cfg.Issuer = strings.TrimRight(cfg.Issuer, "/")
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newFetchClient()
	}
	return &Service{cfg: cfg, st: st, identity: identity, http: client, registry: map[string]*bucket{}}
}

// Enabled reports whether the server can run: it needs an absolute issuer.
func (s *Service) Enabled() bool { return s != nil && s.cfg.Issuer != "" }

// Handler serves the OAuth endpoints and the discovery documents. Mount it at
// /oauth/ and /.well-known/.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleServerMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/", s.handleServerMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/", s.handleResourceMetadata)
	mux.HandleFunc("OPTIONS /.well-known/", handleCORS)
	mux.HandleFunc("POST "+PathRegister, s.handleRegister)
	mux.HandleFunc("OPTIONS "+PathRegister, handleCORS)
	mux.HandleFunc("GET "+PathAuthorize, s.handleAuthorize)
	mux.HandleFunc("POST "+PathAuthorize, s.handleAuthorizeDecision)
	mux.HandleFunc("POST "+PathToken, s.handleToken)
	mux.HandleFunc("OPTIONS "+PathToken, handleCORS)
	mux.HandleFunc("POST "+PathRevoke, s.handleRevoke)
	mux.HandleFunc("OPTIONS "+PathRevoke, handleCORS)
	return mux
}

// ResourceMetadataURL is the RFC 9728 metadata URL of the MCP resource path;
// the 401 challenge points clients to it.
func (s *Service) ResourceMetadataURL(resourcePath string) string {
	return s.cfg.Issuer + "/.well-known/oauth-protected-resource" + resourcePath
}

// ResourceURL is the canonical resource identifier of an MCP path.
func (s *Service) ResourceURL(resourcePath string) string { return s.cfg.Issuer + resourcePath }

// --- discovery ---------------------------------------------------------------

func (s *Service) handleServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeMetadata(w, map[string]any{
		"issuer":                                s.cfg.Issuer,
		"authorization_endpoint":                s.cfg.Issuer + PathAuthorize,
		"token_endpoint":                        s.cfg.Issuer + PathToken,
		"registration_endpoint":                 s.cfg.Issuer + PathRegister,
		"revocation_endpoint":                   s.cfg.Issuer + PathRevoke,
		"scopes_supported":                      auth.ScopeNames(),
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		// draft-ietf-oauth-client-id-metadata-document
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	})
}

func (s *Service) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	const prefix = "/.well-known/oauth-protected-resource"
	resourcePath := strings.TrimPrefix(r.URL.Path, prefix)
	if resourcePath != ResourcePath {
		http.NotFound(w, r)
		return
	}
	writeMetadata(w, map[string]any{
		"resource":                 s.ResourceURL(resourcePath),
		"authorization_servers":    []string{s.cfg.Issuer},
		"scopes_supported":         auth.ScopeNames(),
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Cublyverse MCP",
	})
}

// ResourcePath is the one protected resource: the MCP endpoint.
const ResourcePath = "/mcp"

func handleCORS(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
}

func writeMetadata(w http.ResponseWriter, doc map[string]any) {
	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(doc)
}

// --- helpers ------------------------------------------------------------------

func (s *Service) now() time.Time { return s.cfg.Clock() }

func randomToken(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// grantedScopes keeps the known scopes of a request in registry order. An empty
// request grants the read-only profile scope.
func grantedScopes(requested string) []string {
	want := map[string]bool{}
	for _, scope := range strings.Fields(requested) {
		if auth.ValidScope(scope) {
			want[scope] = true
		}
	}
	var out []string
	for _, scope := range auth.ScopeNames() {
		if want[scope] {
			out = append(out, scope)
		}
	}
	if len(out) == 0 {
		out = []string{auth.ScopeProfileRead}
	}
	return out
}

// Token is a verified access token.
type Token struct {
	ID       string
	ClientID string
	UserID   string
	Scopes   []string
	Resource string
}

// ErrInvalidToken means the access token is unknown, expired or revoked.
var ErrInvalidToken = errors.New("oauth: invalid token")

// Verify checks an access token presented to the MCP endpoint.
func (s *Service) Verify(ctx context.Context, raw string) (Token, error) {
	if !strings.HasPrefix(raw, AccessTokenPrefix) {
		return Token{}, ErrInvalidToken
	}
	row, err := s.st.Queries().GetOAuthTokenByAccess(ctx, hashSecret(raw))
	if err != nil {
		return Token{}, ErrInvalidToken
	}
	if row.RevokedAt.Valid || !row.AccessExpiresAt.After(s.now()) {
		return Token{}, ErrInvalidToken
	}
	_ = s.st.Queries().TouchOAuthToken(ctx, row.ID)
	return Token{
		ID: workspace.FormatID(row.ID), ClientID: row.ClientID, UserID: workspace.FormatID(row.UserID),
		Scopes: row.Scopes, Resource: row.Resource,
	}, nil
}

// Grant is a live authorization shown in the WebApp.
type Grant struct {
	ID         string
	ClientName string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// Grants lists the live authorizations of a user.
func (s *Service) Grants(ctx context.Context, userID string) ([]Grant, error) {
	uid, err := workspace.ParseID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListOAuthGrantsForUser(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	out := make([]Grant, 0, len(rows))
	for _, r := range rows {
		g := Grant{ID: workspace.FormatID(r.FamilyID), ClientName: r.ClientName, Scopes: r.Scopes, CreatedAt: r.CreatedAt}
		if r.LastUsedAt.Valid {
			t := r.LastUsedAt.Time
			g.LastUsedAt = &t
		}
		out = append(out, g)
	}
	return out, nil
}

// RevokeGrant revokes every token of an authorization. A foreign or unknown id
// reports false.
func (s *Service) RevokeGrant(ctx context.Context, userID, grantID string) (bool, error) {
	uid, err := workspace.ParseID(userID)
	if err != nil {
		return false, err
	}
	gid, err := workspace.ParseID(grantID)
	if err != nil {
		return false, nil
	}
	n, err := s.st.Queries().RevokeOAuthFamilyForUser(ctx, db.RevokeOAuthFamilyForUserParams{FamilyID: gid, UserID: uid})
	if err != nil {
		return false, fmt.Errorf("revoke grant: %w", err)
	}
	return n > 0, nil
}

// queryEscape builds a relative URL with a properly escaped query.
func relativeURL(path string, q url.Values) string { return path + "?" + q.Encode() }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
