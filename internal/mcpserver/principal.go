package mcpserver

import (
	"context"

	"github.com/maksimkurb/cublyverse/internal/auth"
)

// Principal is the user behind an MCP request, resolved from the personal
// access token by Handler.
type Principal struct {
	// ID is the principal id (users.id) the tools act for.
	ID string
	// Scopes are the scopes of the token.
	Scopes []string
	// WorkspaceID is the workspace of the endpoint the request arrived at
	// (/mcp/w/{id}), already checked for membership; empty on /mcp.
	WorkspaceID string
	// TokenID is the api_tokens.id of the token, for audit messages.
	TokenID string
}

// HasScope reports whether the token may use an operation guarded by scope. A
// token with any scope may read its own profile, mirroring the HTTP rule that
// every grant implies profile:read.
func (p Principal) HasScope(scope string) bool {
	if scope == auth.ScopeProfileRead && len(p.Scopes) > 0 {
		return true
	}
	for _, granted := range p.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

// principalContextKey stores the authenticated principal of a request.
type principalContextKey struct{}

// WithPrincipal stores the principal in a context. Handler does it for HTTP
// requests; tests do it for in-memory sessions.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the principal stored by Handler.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
