// Package app assembles the process: configuration, database, HTTP server,
// features and background jobs.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/maksimkurb/cublyverse/api"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/version"
	webassets "github.com/maksimkurb/cublyverse/web"
)

const (
	// shutdownTimeout is how long in-flight requests get to finish after a
	// termination signal.
	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 2 * time.Minute
)

// App is a fully wired process.
type App struct {
	cfg      config.Config
	logger   *slog.Logger
	store    *store.Store
	registry *Registry
	jobs     *Runner
	server   *http.Server
	handler  http.Handler

	closeOnce sync.Once
}

// New opens the database, applies migrations, wires the features and builds
// the HTTP handler.
//
// Only DATABASE_URL is required: messenger transports that are not configured
// are skipped with a warning, so a bare database plus `serve` is enough to run
// the API and the WebApp.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = cfg.NewLogger(nopWriter{})
	}

	// Migrations run before the pool is opened so a fresh database works with
	// a single start.
	if err := store.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
		return nil, err
	}

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	registry := NewRegistry()
	logger.Info("features registered", slog.Any("features", registry.Names()))

	if !cfg.TelegramEnabled() {
		logger.Warn("telegram transport disabled: TELEGRAM_BOT_TOKEN is not set")
	}
	if !cfg.MatrixEnabled() {
		logger.Warn("matrix transport disabled: MATRIX_HOMESERVER is not set")
	}

	handler := httpapi.New(httpapi.Deps{
		Version: version.String(),
		Logger:  logger,
		Ready:   st.Ping,
		Web:     webassets.Handler(),
		Spec:    api.OpenAPISpec,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	return &App{
		cfg:      cfg,
		logger:   logger,
		store:    st,
		registry: registry,
		jobs:     NewRunner(logger),
		server:   server,
		handler:  handler,
	}, nil
}

// Handler returns the root HTTP handler. Tests use it without opening a
// listening socket.
func (a *App) Handler() http.Handler { return a.handler }

// Store returns the database store.
func (a *App) Store() *store.Store { return a.store }

// Run starts the background jobs and the HTTP server, then blocks until ctx is
// cancelled or the server fails. Shutdown waits up to shutdownTimeout for
// in-flight requests.
func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.jobs.Start(runCtx, a.registry.Jobs())

	serveErr := make(chan error, 1)
	go func() {
		a.logger.Info("http server listening", slog.String("addr", a.server.Addr))
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("http server: %w", err)
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
		a.logger.Info("shutdown requested")
	case err := <-serveErr:
		runErr = err
		a.logger.Error("http server failed", slog.String("error", err.Error()))
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		a.logger.Error("http server shutdown failed", slog.String("error", err.Error()))
		if runErr == nil {
			runErr = fmt.Errorf("shutdown http server: %w", err)
		}
	}

	cancel()
	a.jobs.Wait()
	return runErr
}

// Close releases the database pool. It is safe to call more than once.
func (a *App) Close() {
	a.closeOnce.Do(func() {
		if a.store != nil {
			a.store.Close()
		}
	})
}

// nopWriter discards logger output; it only exists for the defensive nil-logger
// branch in New.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
