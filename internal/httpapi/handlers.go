package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// handlers implements the generated strict server interface and the probe
// endpoints. It performs no business logic of its own; operations delegate to
// feature services.
type handlers struct {
	deps Deps
}

var _ gen.StrictServerInterface = (*handlers)(nil)

// GetHealth implements GET /api/v1/health.
func (h *handlers) GetHealth(_ context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{
		Status:  "ok",
		Version: h.deps.Version,
	}, nil
}

// readinessResponse is the body of /readyz. The probe is infrastructure
// facing, so it is not part of the public OpenAPI document.
type readinessResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// healthz reports that the process is alive. It never touches dependencies.
func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// readyz reports whether the service can serve traffic. The database is the
// only hard dependency, so an unreachable database means 503.
func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	if h.deps.Ready != nil {
		if err := h.deps.Ready(r.Context()); err != nil {
			h.deps.Logger.WarnContext(r.Context(), "readiness check failed",
				slog.String("error", err.Error()),
				slog.String("request_id", RequestIDFrom(r.Context())),
			)
			writeJSON(w, http.StatusServiceUnavailable, readinessResponse{
				Status:  "unavailable",
				Message: "База данных недоступна",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, readinessResponse{Status: "ok"})
}
