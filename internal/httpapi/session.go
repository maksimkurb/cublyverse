package httpapi

import (
	"context"
	"mime"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/auth"
)

// sessionContextKey stores the authenticated session of the request.
const sessionContextKey contextKey = 1

// withSession adds an authenticated session to the context.
func withSession(ctx context.Context, session auth.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// sessionFrom returns the session stored by sessionMiddleware.
func sessionFrom(ctx context.Context) (auth.Session, bool) {
	session, ok := ctx.Value(sessionContextKey).(auth.Session)
	return session, ok
}

// sessionMiddleware authenticates API requests.
//
// It never rejects a request just because it is anonymous: the sign-in endpoint
// and the health probe are public, and the session-protected handlers answer
// 401 themselves. What it does enforce is the CSRF rule for cookie sessions —
// together with SameSite=Lax, an HTML form cannot forge a mutating request
// because it cannot send application/json.
func (h *handlers) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.deps.Auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		session, ok := h.deps.Auth.ResolveSession(r.Context(), r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if session.FromCookie && mutatesState(r.Method) && !isJSONRequest(r) {
			writeError(w, http.StatusForbidden, codeForbidden,
				"Изменяющие запросы с cookie-сессией должны быть в формате application/json")
			return
		}
		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
	})
}

// mutatesState reports whether a method changes server state.
func mutatesState(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

// isJSONRequest reports whether the request declares a JSON body.
func isJSONRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}
