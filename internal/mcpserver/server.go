// Package mcpserver hosts the MCP (Model Context Protocol) endpoint of
// Cublyverse.
//
// The endpoint is mounted at /mcp and speaks Streamable HTTP. It is
// deliberately stateless: every JSON-RPC message arrives in its own HTTP
// request, the server keeps no session between calls and does not offer a
// standalone SSE stream. That matches the shape of MCP clients (Claude Code,
// Claude Desktop connectors, scripts), makes a restart invisible to a client
// and lets several replicas serve the same client without sticky sessions.
//
// Authentication is PAT only: an MCP client is not a browser, so it cannot own
// a cookie session, and accepting cookies here would mean accepting an ambient
// credential on an endpoint designed to be called by a machine. A personal
// access token is explicit, revocable and scoped; see docs/MCP.md.
//
// Tools are registered by the features through feature.Feature.RegisterMCP, so
// every feature exposes its service to MCP exactly as it does to the bot and
// the HTTP API.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/oauthserver"
)

// ImplementationName is the server name reported during initialization.
const ImplementationName = "cublyverse"

// Deps are the collaborators the MCP server needs.
type Deps struct {
	// Version is the build version reported to clients.
	Version string
	// Logger receives transport and tool errors. Nil discards them.
	Logger *slog.Logger
	// Profile loads the profile behind whoami. Nil makes whoami report only the
	// principal id and the scopes of the request.
	Profile func(ctx context.Context, principalID string) (auth.Profile, error)
	// Authenticate resolves the personal access token of an HTTP request. It is
	// called by Handler for every /mcp request; a nil function rejects
	// everything.
	Authenticate func(ctx context.Context, r *http.Request) (auth.Session, bool)
	// OAuth verifies OAuth access tokens (cv_oat_…) issued by the built-in
	// authorization server. Nil disables OAuth: only personal access tokens
	// work and the 401 challenge carries no resource metadata.
	OAuth OAuthVerifier
	// Spaces lists the spaces of a user for the spaces_list tool. Nil makes the
	// tool answer with an empty list.
	Spaces func(ctx context.Context, userID string) ([]Space, error)
}

// OAuthVerifier is the part of the authorization server the MCP endpoint needs.
type OAuthVerifier interface {
	// Verify checks an access token.
	Verify(ctx context.Context, raw string) (oauthserver.Token, error)
	// ResourceMetadataURL is where clients find the protected resource metadata
	// of the MCP path.
	ResourceMetadataURL(resourcePath string) string
	// ResourceURL is the canonical resource identifier of an MCP path.
	ResourceURL(resourcePath string) string
}

// Server is the MCP server of the process. Features register their tools on it
// once at startup.
type Server struct {
	deps Deps
	sdk  *mcp.Server

	mu    sync.Mutex
	names []string
	// handler is built lazily so tests can register tools after New.
	handler http.Handler
}

// New builds the server and registers the core tools (whoami). Features add
// theirs with RegisterMCP.
func New(deps Deps) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	s := &Server{
		deps: deps,
		sdk:  mcp.NewServer(&mcp.Implementation{Name: ImplementationName, Version: deps.Version}, nil),
	}
	s.registerCore()
	return s
}

// SDK exposes the underlying SDK server. Tests use it with the in-memory
// transport; nothing else should need it.
func (s *Server) SDK() *mcp.Server { return s.sdk }

// Tools returns the names of the registered tools in registration order.
func (s *Server) Tools() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.names...)
}

// AddTool registers a typed tool. It is the one entry point features use, so
// the tool list stays introspectable for the documentation check.
func AddTool[In, Out any](s *Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	s.mu.Lock()
	s.names = append(s.names, tool.Name)
	s.mu.Unlock()
	mcp.AddTool(s.sdk, tool, handler)
}

// Handler returns the authenticated Streamable HTTP handler.
func (s *Server) Handler() http.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handler != nil {
		return s.handler
	}
	transport := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s.sdk },
		&mcp.StreamableHTTPOptions{
			Stateless: true,
			Logger:    s.deps.Logger,
			// The service is normally reached through a reverse proxy, which
			// connects over loopback while the public Host header is not
			// loopback: the SDK's localhost protection would reject exactly the
			// production setup. A PAT is not an ambient credential, so DNS
			// rebinding gains nothing here.
			DisableLocalhostProtection: true,
		},
	)
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// There is one MCP resource; tools name the space they work with.
		resourcePath := strings.TrimSuffix(r.URL.Path, "/")
		if resourcePath != oauthserver.ResourcePath {
			http.NotFound(w, r)
			return
		}
		principal, ok := s.authenticate(w, r, resourcePath)
		if !ok {
			return
		}
		transport.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
	return s.handler
}

// authenticate resolves the bearer token of an MCP request: an OAuth access
// token or a personal access token. On failure it answers the request and
// returns false.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, resourcePath string) (Principal, bool) {
	challenge := `Bearer realm="cublyverse-mcp"`
	if s.deps.OAuth != nil {
		challenge += `, resource_metadata="` + s.deps.OAuth.ResourceMetadataURL(resourcePath) + `"`
	}
	raw := auth.BearerToken(r)
	if s.deps.OAuth != nil && strings.HasPrefix(raw, oauthserver.AccessTokenPrefix) {
		token, err := s.deps.OAuth.Verify(r.Context(), raw)
		if err != nil {
			writeUnauthorized(w, challenge+`, error="invalid_token"`, "Недействительный токен доступа")
			return Principal{}, false
		}
		// The token is bound to the resource it was issued for (RFC 8707).
		if token.Resource != "" && token.Resource != s.deps.OAuth.ResourceURL(resourcePath) {
			writeUnauthorized(w, challenge+`, error="invalid_token"`, "Токен выдан для другого ресурса")
			return Principal{}, false
		}
		return Principal{ID: token.UserID, Scopes: token.Scopes, TokenID: token.ID}, true
	}
	if s.deps.Authenticate == nil {
		writeUnauthorized(w, challenge, "Требуется токен доступа")
		return Principal{}, false
	}
	session, ok := s.deps.Authenticate(r.Context(), r)
	if !ok || !session.IsAPIToken() {
		writeUnauthorized(w, challenge, "Недействительный токен доступа")
		return Principal{}, false
	}
	return Principal{ID: session.PrincipalID, Scopes: append([]string(nil), session.Scopes...), TokenID: session.APITokenID}, true
}

// writeUnauthorized answers an MCP HTTP request with the shared JSON error
// shape. MCP clients show the body next to the 401, which is more useful than a
// bare status line. The challenge tells OAuth clients where to start.
func writeUnauthorized(w http.ResponseWriter, challenge, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", challenge)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    "unauthorized",
		"message": message,
	})
}

// ErrUnauthenticated is returned by a tool when the request carries no
// principal. The handler is only reachable after authentication, so this is a
// wiring bug rather than a client error.
var ErrUnauthenticated = errors.New("request is not authenticated")

// RequireScope returns a tool error when the principal lacks scope. Tool errors
// become ordinary MCP results with isError set, so the model can read the
// message and correct itself.
func RequireScope(principal Principal, scope string) error {
	if principal.HasScope(scope) {
		return nil
	}
	return errors.New("insufficient scope: this tool requires " + scope)
}
