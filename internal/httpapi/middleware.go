package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// requestIDHeader carries the correlation id of a single request.
const requestIDHeader = "X-Request-Id"

type contextKey int

const requestIDContextKey contextKey = 0

// RequestIDFrom returns the request id stored in ctx, or "" when absent.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey).(string)
	return id
}

// RequestID makes sure every request has a correlation id. A client supplied id
// is reused when it looks sane, otherwise a fresh one is generated. The id is
// echoed in the response header and added to every log line.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey, id)))
	})
}

// validRequestID accepts conservative client provided ids and rejects anything
// that could be used to inject a forged log field.
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand never fails on supported platforms; fall back to a
		// timestamp so the id is still unique enough for correlation.
		return "t" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf[:])
}

// responseRecorder remembers the status code and response size for logging.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// LogRequest logs one structured line per request.
func LogRequest(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		attrs := []slog.Attr{
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Int64("bytes", recorder.bytes),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("remote_addr", r.RemoteAddr),
		}
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.LogAttrs(r.Context(), level, "http request", attrs...)
	})
}

// Recover turns a panicking handler into a 500 response with the shared JSON
// error payload, and records the stack trace in the log.
func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// http.ErrAbortHandler is the documented way to abort a response;
			// it must keep propagating.
			if err, ok := recovered.(error); ok && err == http.ErrAbortHandler {
				panic(recovered)
			}
			logger.ErrorContext(r.Context(), "panic in http handler",
				slog.Any("panic", recovered),
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("stack", string(debug.Stack())),
			)
			writeError(w, http.StatusInternalServerError, codeInternal, "Внутренняя ошибка сервера")
		}()
		next.ServeHTTP(w, r)
	})
}
