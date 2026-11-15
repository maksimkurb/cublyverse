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
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
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
