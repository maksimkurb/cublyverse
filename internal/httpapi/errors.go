package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// writeJSON serialises v as the response body. It is only used for responses
// produced by this package; generated handlers encode their own payloads.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the shared Error payload declared in api/openapi.yaml.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, gen.Error{Code: code, Message: message})
}
