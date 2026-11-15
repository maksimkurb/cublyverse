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
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
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
		if s.deps.Authenticate == nil {
			writeUnauthorized(w, "Требуется персональный токен доступа")
			return
		}
		session, ok := s.deps.Authenticate(r.Context(), r)
		if !ok || !session.IsAPIToken() {
			writeUnauthorized(w, "Недействительный токен доступа")
			return
		}
		principal := Principal{
			ID:      session.PrincipalID,
			Scopes:  append([]string(nil), session.Scopes...),
			TokenID: session.APITokenID,
		}
		transport.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
	return s.handler
}

// writeUnauthorized answers an MCP HTTP request with the shared JSON error
// shape. MCP clients show the body next to the 401, which is more useful than a
// bare status line.
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="cublyverse-mcp"`)
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
