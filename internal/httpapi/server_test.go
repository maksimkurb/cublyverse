package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/api"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newServer(t *testing.T, deps httpapi.Deps) *httptest.Server {
	t.Helper()
	if deps.Logger == nil {
		deps.Logger = discardLogger()
	}
	if deps.Version == "" {
		deps.Version = "test"
	}
	if deps.Spec == nil {
		deps.Spec = api.OpenAPISpec
	}
	server := httptest.NewServer(httpapi.New(deps))
	t.Cleanup(server.Close)
	return server
}

func get(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func decodeError(t *testing.T, body []byte) gen.Error {
	t.Helper()
	var payload gen.Error
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error payload %q: %v", body, err)
	}
	return payload
}

func TestGetHealth(t *testing.T) {
	server := newServer(t, httpapi.Deps{Version: "1.2.3"})

	resp, body := get(t, server.URL+"/api/v1/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, body)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/json"; !strings.HasPrefix(got, want) {
		t.Errorf("Content-Type = %q, want prefix %q", got, want)
	}

	var health gen.Health
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("decode health %q: %v", body, err)
	}
	if health.Status != "ok" {
		t.Errorf("status field = %q, want %q", health.Status, "ok")
	}
	if health.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", health.Version, "1.2.3")
	}
}

func TestUnknownPathReturnsJSONError(t *testing.T) {
	server := newServer(t, httpapi.Deps{})

	resp, body := get(t, server.URL+"/api/v1/does-not-exist")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/json"; !strings.HasPrefix(got, want) {
		t.Errorf("Content-Type = %q, want prefix %q", got, want)
	}
	payload := decodeError(t, body)
	if payload.Code != "not_found" {
		t.Errorf("code = %q, want %q", payload.Code, "not_found")
	}
	if payload.Message == "" {
		t.Error("message is empty")
	}
}

func TestPanicReturnsJSONError(t *testing.T) {
	server := newServer(t, httpapi.Deps{
		Web: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}),
	})

	resp, body := get(t, server.URL+"/app/anything")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	payload := decodeError(t, body)
	if payload.Code != "internal" {
		t.Errorf("code = %q, want %q", payload.Code, "internal")
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		ready      func(context.Context) error
		wantStatus int
		wantState  string
	}{
		{
			name:       "database reachable",
			ready:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantState:  "ok",
		},
		{
			name:       "database unavailable",
			ready:      func(context.Context) error { return errors.New("connection refused") },
			wantStatus: http.StatusServiceUnavailable,
			wantState:  "unavailable",
		},
		{
			name:       "no readiness check configured",
			ready:      nil,
			wantStatus: http.StatusOK,
			wantState:  "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newServer(t, httpapi.Deps{Ready: tt.ready})
			resp, body := get(t, server.URL+"/readyz")
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tt.wantStatus, body)
			}
			var payload struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode readiness payload %q: %v", body, err)
			}
			if payload.Status != tt.wantState {
				t.Errorf("status field = %q, want %q", payload.Status, tt.wantState)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	server := newServer(t, httpapi.Deps{})
	resp, body := get(t, server.URL+"/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "ok\n" {
		t.Errorf("body = %q, want %q", body, "ok\n")
	}
}

func TestOpenAPISpecIsServed(t *testing.T) {
	server := newServer(t, httpapi.Deps{})

	resp, body := get(t, server.URL+"/api/openapi.yaml")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/yaml"; !strings.HasPrefix(got, want) {
		t.Errorf("Content-Type = %q, want prefix %q", got, want)
	}
	if !strings.Contains(string(body), "openapi: 3.0.3") {
		t.Errorf("served document does not look like the OpenAPI 3.0.3 spec:\n%s", body)
	}
	if !strings.Contains(string(body), "operationId: getHealth") {
		t.Error("served document does not contain the getHealth operation")
	}
}

func TestDocsPage(t *testing.T) {
	server := newServer(t, httpapi.Deps{})

	for _, path := range []string{"/api/docs", "/api/docs/"} {
		resp, body := get(t, server.URL+path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}
		if got, want := resp.Header.Get("Content-Type"), "text/html"; !strings.HasPrefix(got, want) {
			t.Errorf("GET %s: Content-Type = %q, want prefix %q", path, got, want)
		}
		if !strings.Contains(string(body), "/api/openapi.yaml") {
			t.Errorf("GET %s: page does not reference the spec", path)
		}
	}
}

func TestRootRedirectsToWebApp(t *testing.T) {
	server := newServer(t, httpapi.Deps{})

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got, want := resp.Header.Get("Location"), "/app/"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestRequestIDHeader(t *testing.T) {
	server := newServer(t, httpapi.Deps{})

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Request-Id", "client-id-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got, want := resp.Header.Get("X-Request-Id"), "client-id-1"; got != want {
		t.Errorf("X-Request-Id = %q, want %q", got, want)
	}

	// An unsafe id must be replaced, never echoed back.
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/v1/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Request-Id", "bad id with spaces")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("X-Request-Id"); got == "" || got == "bad id with spaces" {
		t.Errorf("X-Request-Id = %q, want a generated id", got)
	}
}
