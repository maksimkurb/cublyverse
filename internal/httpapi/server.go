// Package httpapi implements the HTTP edge of the service: the generated
// OpenAPI server, the uniform error format, shared middleware, the health
// probes, the API documentation and the WebApp files.
//
// Handlers stay thin: business logic belongs to the feature services, which
// these handlers call.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
	"github.com/maksimkurb/cublyverse/internal/oauthserver"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

const (
	// apiBaseURL is the prefix of every operation declared in api/openapi.yaml.
	apiBaseURL = "/api/v1"
	// redirectTarget is where the site root sends the browser.
	redirectTarget = "/app/"
)

// Stable error codes used by the API. They are part of the contract and must
// not change; the human readable message may.
const (
	codeBadRequest      = "bad_request"
	codeNotFound        = "not_found"
	codeInternal        = "internal"
	codeUnauthorized    = "unauthorized"
	codeForbidden       = "forbidden"
	codeConflict        = "conflict"
	codeInvalidInitData = "invalid_init_data"
	codeUnavailable     = "unavailable"
)

// AuthService is the identity service the API depends on. It is an interface so
// the tests can substitute it, while the real implementation is *auth.Service.
type AuthService interface {
	// ResolveSession authenticates a request through the Bearer header or the
	// session cookie.
	ResolveSession(ctx context.Context, r *http.Request) (auth.Session, bool)
	// Profile returns the user and their messenger bindings.
	Profile(ctx context.Context, principalID string) (auth.Profile, error)
	// UpdateTimezone stores the user's IANA time zone.
	UpdateTimezone(ctx context.Context, principalID, timezone string) (auth.Profile, error)
	// RevokeSession invalidates the session of the current request.
	RevokeSession(ctx context.Context, sessionID string) error
	// AuthenticateTelegram exchanges Mini App initData for a session.
	AuthenticateTelegram(ctx context.Context, initData string) (auth.TelegramAuthResult, error)
	// ClearedSessionCookie serialises an expired session cookie.
	ClearedSessionCookie() string
	// ListAPITokens returns every personal access token of a user.
	ListAPITokens(ctx context.Context, principalID string) ([]auth.APIToken, error)
	// CreateAPIToken issues a token and returns its only plaintext copy.
	CreateAPIToken(ctx context.Context, principalID, name string, scopes []string, expiresInDays *int) (auth.APIToken, string, error)
	// RevokeAPIToken invalidates a token of the user.
	RevokeAPIToken(ctx context.Context, principalID, id string) error
}

// ConnectionService lists and revokes OAuth authorizations of a user. The real
// implementation is *oauthserver.Service.
type ConnectionService interface {
	// Grants lists the live authorizations of a user.
	Grants(ctx context.Context, userID string) ([]oauthserver.Grant, error)
	// RevokeGrant revokes one; false means unknown or foreign.
	RevokeGrant(ctx context.Context, userID, grantID string) (bool, error)
}

// WorkspaceService is the workspace business logic. The real implementation is
// *workspace.Service.
type WorkspaceService interface {
	// Create makes a workspace owned by the user.
	Create(ctx context.Context, userID, name string) (workspace.Workspace, error)
	// List returns the workspaces of the user.
	List(ctx context.Context, userID string) ([]workspace.Workspace, error)
	// Rename changes the name of a workspace.
	Rename(ctx context.Context, userID, workspaceID, name string) (workspace.Workspace, error)
	// Delete removes a workspace with everything in it.
	Delete(ctx context.Context, userID, workspaceID string) error
	// Members lists the members of a workspace.
	Members(ctx context.Context, userID, workspaceID string) ([]workspace.Member, error)
	// Candidates lists users who can still be added.
	Candidates(ctx context.Context, userID, workspaceID, query string) ([]workspace.Candidate, error)
	// AddMember adds an existing user; a repeated add is a no-op.
	AddMember(ctx context.Context, userID, workspaceID, newUserID string) error
	// RemoveMember removes one; the last member cannot be removed.
	RemoveMember(ctx context.Context, userID, workspaceID, memberID string) error
}

// CraftService is the craft supplies business logic. The real implementation is
// *craft.Service.
type CraftService interface {
	// Categories lists the material categories.
	Categories(ctx context.Context) ([]craft.Category, error)
	// Manufacturers lists the known manufacturers.
	Manufacturers(ctx context.Context) ([]craft.Manufacturer, error)
	// SearchCatalog searches the global catalogue and private items.
	SearchCatalog(ctx context.Context, userID, workspaceID string, filter craft.CatalogFilter) ([]craft.Item, error)
	// GetItem returns one visible catalogue item.
	GetItem(ctx context.Context, userID, workspaceID, itemID string) (craft.Item, error)
	// CreateItem adds a private catalogue item.
	CreateItem(ctx context.Context, userID, workspaceID string, input craft.ItemInput) (craft.Item, error)
	// UpdateItem replaces a private catalogue item.
	UpdateItem(ctx context.Context, userID, workspaceID, itemID string, input craft.ItemInput) (craft.Item, error)
	// DeleteItem removes a private item with its stock.
	DeleteItem(ctx context.Context, userID, workspaceID, itemID string) error
	// LinkToCatalog moves the stock of a private item to another item.
	LinkToCatalog(ctx context.Context, userID, workspaceID, privateID, targetID string) (craft.StockEntry, error)
	// ListInventory lists the stock of the workspace.
	ListInventory(ctx context.Context, userID, workspaceID string, filter craft.CatalogFilter) ([]craft.StockEntry, error)
	// SetQty sets the stock to an absolute value.
	SetQty(ctx context.Context, userID, workspaceID, itemID string, qty float64, reason string) (craft.StockEntry, error)
	// Adjust changes the stock by a delta.
	Adjust(ctx context.Context, userID, workspaceID, itemID string, delta float64, reason string) (craft.StockEntry, error)
	// UpdateStockMeta changes the location and notes of a stock row.
	UpdateStockMeta(ctx context.Context, userID, workspaceID, itemID, location, notes string) error
	// RemoveFromInventory removes a stock row.
	RemoveFromInventory(ctx context.Context, userID, workspaceID, itemID string) error
	// ListWishes returns the wish list of a workspace, filtered by status.
	ListWishes(ctx context.Context, userID, workspaceID string, filter craft.WishFilter) ([]craft.Wish, error)
	// AddWish adds a wish list entry: a catalogue item or free text.
	AddWish(ctx context.Context, userID, workspaceID string, input craft.WishInput) (craft.Wish, error)
	// UpdateWish changes the quantity, priority, note and status of an entry.
	UpdateWish(ctx context.Context, userID, workspaceID, wishID string, input craft.WishUpdate) (craft.Wish, error)
	// DeleteWish removes an entry with its shop links.
	DeleteWish(ctx context.Context, userID, workspaceID, wishID string) error
	// AddWishLink attaches a shop page to a wish.
	AddWishLink(ctx context.Context, userID, workspaceID, wishID string, input craft.WishLinkInput) (craft.WishLink, error)
	// RemoveWishLink removes one shop page of a wish.
	RemoveWishLink(ctx context.Context, userID, workspaceID, wishID, linkID string) error
	// MarkBought marks a wish as bought, optionally adding it to the stock.
	MarkBought(ctx context.Context, userID, workspaceID, wishID string, addToStock bool) (craft.Wish, error)
	// CreateProject creates an empty project.
	CreateProject(ctx context.Context, userID, workspaceID string, input craft.ProjectInput) (craft.Project, error)
	// ListProjects returns the projects of a workspace.
	ListProjects(ctx context.Context, userID, workspaceID string) ([]craft.Project, error)
	// GetProject returns one project with its materials.
	GetProject(ctx context.Context, userID, workspaceID, projectID string) (craft.Project, error)
	// UpdateProject replaces the name and notes of a project.
	UpdateProject(ctx context.Context, userID, workspaceID, projectID string, input craft.ProjectInput) (craft.Project, error)
	// DeleteProject removes a project with its material plan.
	DeleteProject(ctx context.Context, userID, workspaceID, projectID string) error
	// SetProjectMaterial sets the planned quantity of a material.
	SetProjectMaterial(ctx context.Context, userID, workspaceID, projectID, itemID string, qtyPlanned *float64) (craft.Project, error)
	// RemoveProjectMaterial drops a material from a project plan.
	RemoveProjectMaterial(ctx context.Context, userID, workspaceID, projectID, itemID string) error
	// UseMaterial withdraws stock for a project and records the usage.
	UseMaterial(ctx context.Context, userID, workspaceID, projectID, itemID string, qty float64) (craft.Project, error)
	// ShortageToWishlist adds the project shortage to the wish list.
	ShortageToWishlist(ctx context.Context, userID, workspaceID, projectID string) ([]craft.Wish, error)
}

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	// Version is reported by GET /api/v1/health.
	Version string
	// Logger receives request logs and handler errors.
	Logger *slog.Logger
	// Ready reports whether the service can serve traffic (for example, the
	// database is reachable). It backs /readyz.
	Ready func(ctx context.Context) error
	// Web serves the compiled WebApp at /app/. It may be nil, in which case
	// /app/ is not mounted.
	Web http.Handler
	// Spec is the raw OpenAPI document served at /api/openapi.yaml.
	Spec []byte
	// Auth is the identity service. Without it every session-protected
	// operation answers 401 and the sign-in pages are not mounted.
	Auth AuthService
	// AuthPages serves /auth/*: the OIDC round trip, the confirmation page and
	// the Mini App initData check.
	AuthPages http.Handler
	// Reminders serves the reminders feature. Without it the reminders
	// operations answer 500: it is always wired by internal/app.
	Reminders reminders.API
	// MCP serves the Model Context Protocol endpoint at /mcp. It performs its
	// own personal access token authentication, so it is mounted outside the
	// session middleware.
	MCP http.Handler
	// OAuth serves the authorization server of the MCP endpoint: /oauth/* and
	// the /.well-known/ discovery documents. Nil when PUBLIC_BASE_URL is not
	// set, in which case MCP accepts personal access tokens only.
	OAuth http.Handler
	// Connections backs the list of connected applications. Nil (OAuth
	// disabled) makes the list empty.
	Connections ConnectionService
	// Workspaces backs the workspace and member operations. It is always wired
	// by internal/app.
	Workspaces WorkspaceService
	// Craft backs the catalogue, stock and reference operations. It is always
	// wired by internal/app.
	Craft CraftService
}

// New builds the root HTTP handler: API, probes, documentation and the WebApp.
//
// Unknown paths answer with the shared JSON error payload, so clients never
// have to parse an HTML error page.
func New(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	mux := http.NewServeMux()
	h := &handlers{deps: deps}

	// Both error hooks answer with the shared Error payload instead of the
	// generated plain text, and log the reason.
	rejectRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		deps.Logger.WarnContext(r.Context(), "http request rejected",
			slog.String("error", err.Error()),
			slog.String("request_id", RequestIDFrom(r.Context())),
		)
		writeError(w, http.StatusBadRequest, codeBadRequest, "Некорректный запрос")
	}
	failRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		deps.Logger.ErrorContext(r.Context(), "http handler failed",
			slog.String("error", err.Error()),
			slog.String("request_id", RequestIDFrom(r.Context())),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "Внутренняя ошибка сервера")
	}

	// The generated operations live on their own mux so the session middleware
	// only touches /api/v1/* and never static files. The base path is stripped
	// by the mount below, so the generated patterns are "/health", "/me", …
	apiMux := http.NewServeMux()
	strict := gen.NewStrictHandlerWithOptions(h, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  rejectRequest,
		ResponseErrorHandlerFunc: failRequest,
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseRouter:       apiMux,
		ErrorHandlerFunc: rejectRequest,
	})
	// Keep the JSON error contract for API paths the specification does not
	// describe.
	apiMux.HandleFunc("/", notFound)
	mux.Handle("/api/v1/", http.StripPrefix(apiBaseURL, h.sessionMiddleware(apiMux)))

	if deps.AuthPages != nil {
		mux.Handle("/auth/", deps.AuthPages)
	}

	// The MCP endpoint authenticates personal access tokens itself and therefore
	// sits outside the API prefix and its session middleware.
	if deps.MCP != nil {
		mux.Handle("/mcp", deps.MCP)
		mux.Handle("/mcp/", deps.MCP)
	}

	if deps.OAuth != nil {
		mux.Handle("/oauth/", deps.OAuth)
		mux.Handle("/.well-known/", deps.OAuth)
	}

	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /api/openapi.yaml", h.openapi)
	mux.HandleFunc("GET /api/docs", h.docs)
	mux.HandleFunc("GET /api/docs/", h.docs)

	if deps.Web != nil {
		mux.Handle("/app/", http.StripPrefix("/app", deps.Web))
		mux.HandleFunc("GET /app", redirectTo(redirectTarget))
	}

	// "/{$}" matches only the site root, while "/" catches every path that no
	// other pattern handles and answers with the JSON error payload.
	mux.HandleFunc("GET /{$}", redirectTo(redirectTarget))
	mux.HandleFunc("/", notFound)

	return RequestID(LogRequest(deps.Logger, Recover(deps.Logger, mux)))
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, codeNotFound, "Страница не найдена")
}

func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}
}
