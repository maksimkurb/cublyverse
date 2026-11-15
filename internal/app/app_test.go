package app_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/app"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// freeAddr reserves and releases a local port so the test can point the server
// at a known address.
func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func statusOf(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestAppLifecycle is the M0-4 acceptance test: the process starts with only
// DATABASE_URL, serves the probes, reports 503 when the database goes away and
// shuts down cleanly when the context is cancelled.
func TestAppLifecycle(t *testing.T) {
	databaseURL := testdb.Start(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Config{
		HTTPAddr:    freeAddr(t),
		DatabaseURL: databaseURL,
		LogFormat:   config.LogFormatText,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with only DATABASE_URL: %v", err)
	}

	application, err := app.New(ctx, cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer application.Close()

	// The same handler powers the real server; check it without a socket first.
	inProcess := httptest.NewServer(application.Handler())
	defer inProcess.Close()

	for _, path := range []string{"/healthz", "/readyz", "/api/v1/health", "/api/openapi.yaml", "/api/docs"} {
		if got := statusOf(t, inProcess.URL+path); got != http.StatusOK {
			t.Errorf("GET %s = %d, want %d", path, got, http.StatusOK)
		}
	}

	// Run serves on a real socket until the context is cancelled.
	runErr := make(chan error, 1)
	go func() { runErr <- application.Run(ctx) }()

	baseURL := "http://" + cfg.HTTPAddr
	waitFor(t, baseURL+"/healthz")

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}

	// Once the pool is closed the readiness probe must fail.
	application.Close()
	if got := statusOf(t, inProcess.URL+"/readyz"); got != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz after Close = %d, want %d", got, http.StatusServiceUnavailable)
	}
	// Liveness must stay green: the process itself is still alive.
	if got := statusOf(t, inProcess.URL+"/healthz"); got != http.StatusOK {
		t.Errorf("GET /healthz after Close = %d, want %d", got, http.StatusOK)
	}
}

// TestAppTransportWiring checks the conditions of the messenger startup: a bot
// token alone is not enough (nobody could sign in), while a wrong OIDC issuer
// fails the start instead of leaving a half-configured service running.
func TestAppTransportWiring(t *testing.T) {
	databaseURL := testdb.Start(t)
	logger := testLogger()

	t.Run("telegram without OIDC keeps the API up", func(t *testing.T) {
		cfg := config.Config{
			HTTPAddr:    freeAddr(t),
			DatabaseURL: databaseURL,
			LogFormat:   config.LogFormatText,
			Telegram:    config.TelegramConfig{BotToken: "123456:test-token"},
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		application, err := app.New(context.Background(), cfg, logger)
		if err != nil {
			t.Fatalf("New with a bot token but no OIDC: %v", err)
		}
		defer application.Close()

		server := httptest.NewServer(application.Handler())
		defer server.Close()
		if got := statusOf(t, server.URL+"/api/v1/health"); got != http.StatusOK {
			t.Errorf("GET /api/v1/health = %d, want %d", got, http.StatusOK)
		}
	})

	t.Run("unreachable OIDC issuer fails the start", func(t *testing.T) {
		cfg := config.Config{
			HTTPAddr:      freeAddr(t),
			DatabaseURL:   databaseURL,
			LogFormat:     config.LogFormatText,
			PublicBaseURL: "https://cublyverse.example.com",
			OIDC: config.OIDCConfig{
				Issuer:   "http://127.0.0.1:1",
				ClientID: "cublyverse",
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if _, err := app.New(context.Background(), cfg, logger); err == nil {
			t.Fatal("New succeeded with an unreachable OIDC issuer")
		}
	})
}

func waitFor(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("server at %s did not become healthy: %v", url, err)
			}
			t.Fatalf("server at %s did not become healthy: status %d", url, resp.StatusCode)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
