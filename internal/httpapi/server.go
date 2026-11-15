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
	codeBadRequest = "bad_request"
	codeNotFound   = "not_found"
	codeInternal   = "internal"
)

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

	strict := gen.NewStrictHandlerWithOptions(h, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  rejectRequest,
		ResponseErrorHandlerFunc: failRequest,
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:          apiBaseURL,
		BaseRouter:       mux,
		ErrorHandlerFunc: rejectRequest,
	})

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
