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
	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	"github.com/maksimkurb/cublyverse/internal/interaction/postgres"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/version"
	unibot "github.com/maksimkurb/cublyverse/unibot"
	matrixtransport "github.com/maksimkurb/cublyverse/unibot/platform/matrix"
	telegramtransport "github.com/maksimkurb/cublyverse/unibot/platform/telegram"
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

	auth    *auth.Service
	runtime *unibot.Runtime

	// extraJobs are the cleanup tasks of the identity layer and the
	// interaction registry; features bring their own through the registry.
	extraJobs []Job

	closeOnce sync.Once
}

// New opens the database, applies migrations, wires the features and builds
// the HTTP handler.
//
// Only DATABASE_URL is required: messenger transports that are not configured
// are skipped with a warning, so a bare database plus `serve` is enough to run
// the API and the WebApp. Transport startup additionally needs OIDC, because an
// unlinked user could not sign in otherwise.
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

	authService, err := auth.New(ctx, st, authConfig(cfg), logger)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("configure auth: %w", err)
	}

	registry := NewRegistry()
	logger.Info("features registered", slog.Any("features", registry.Names()))

	interactions := postgres.New(st)

	application := &App{
		cfg:      cfg,
		logger:   logger,
		store:    st,
		registry: registry,
		jobs:     NewRunner(logger),
		auth:     authService,
	}

	handler := httpapi.New(httpapi.Deps{
		Version:   version.String(),
		Logger:    logger,
		Ready:     st.Ping,
		Web:       webassets.Handler(),
		Spec:      api.OpenAPISpec,
		Auth:      authService,
		AuthPages: authService.Handler(),
	})

	application.server = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	application.handler = handler

	if err := application.startBot(ctx, interactions); err != nil {
		st.Close()
		return nil, err
	}

	// Cleanup tasks are shared by every feature and the identity layer.
	application.extraJobs = []Job{
		authService.CleanupJob(),
		interactions.CleanupJob(logger),
	}

	return application, nil
}

// startBot wires the messenger runtime when a transport and OIDC are
// configured. The bot only replies with the sign-in button in this milestone;
// M2 replaces the handler with the real router.
func (a *App) startBot(ctx context.Context, interactions unibot.InteractionStore) error {
	if !a.cfg.TelegramEnabled() && !a.cfg.MatrixEnabled() {
		if !a.cfg.TelegramEnabled() {
			a.logger.Warn("telegram transport disabled: TELEGRAM_BOT_TOKEN is not set")
		}
		if !a.cfg.MatrixEnabled() {
			a.logger.Warn("matrix transport disabled: MATRIX_HOMESERVER is not set")
		}
		return nil
	}
	if !a.auth.OIDCEnabled() {
		a.logger.Warn("messenger transports disabled: OIDC_ISSUER is not set, so nobody could sign in")
		return nil
	}

	transports, err := a.transports(interactions)
	if err != nil {
		return err
	}
	if len(transports) == 0 {
		return nil
	}

	botHandler := &bot.Handler{
		Links:     a.auth,
		WebAppURL: a.webAppURL(),
		Logger:    a.logger,
	}
	runtime, err := unibot.NewRuntime(a.auth, botHandler.Handle, transports...)
	if err != nil {
		return fmt.Errorf("build messenger runtime: %w", err)
	}
	a.runtime = runtime

	// Tell the user in the chat once the binding is confirmed.
	a.auth.OnLinked = func(ctx context.Context, binding unibot.Binding) {
		message := unibot.Message{Text: "✅ Аккаунт привязан. Откройте приложение кнопкой ниже."}
		if url := a.webAppURL(); url != "" {
			message.Actions = [][]unibot.Action{{{
				ID:    "open_app",
				Label: "Открыть приложение",
				Icon:  "🧩",
				Kind:  unibot.ActionOpenApp,
				Style: unibot.ActionStylePrimary,
				URL:   url,
			}}}
		}
		if _, err := runtime.SendToAddress(ctx, unibot.Address{
			Platform:       binding.Platform,
			RemoteUserID:   binding.RemoteUserID,
			ConversationID: binding.ConversationID,
		}, message); err != nil {
			a.logger.Warn("post-link message failed", "platform", string(binding.Platform), "error", err)
		}
	}
	return nil
}

// transports builds the messenger adapters that are configured.
func (a *App) transports(interactions unibot.InteractionStore) ([]unibot.Transport, error) {
	var transports []unibot.Transport
	if a.cfg.TelegramEnabled() {
		adapter, err := telegramtransport.New(telegramtransport.Config{
			Token: a.cfg.Telegram.BotToken,
		}, interactions)
		if err != nil {
			return nil, fmt.Errorf("configure telegram transport: %w", err)
		}
		transports = append(transports, adapter)
	}
	if a.cfg.MatrixEnabled() {
		adapter, err := matrixtransport.New(matrixtransport.Config{
			Homeserver:   a.cfg.Matrix.Homeserver,
			Username:     a.cfg.Matrix.Username,
			Password:     a.cfg.Matrix.Password,
			CryptoDBPath: a.cfg.Matrix.CryptoDBPath,
			PickleKey:    []byte(a.cfg.Matrix.PickleKey),
		}, interactions)
		if err != nil {
			return nil, fmt.Errorf("configure matrix transport: %w", err)
		}
		transports = append(transports, adapter)
	}
	return transports, nil
}

// webAppURL is the absolute WebApp URL for messenger buttons.
func (a *App) webAppURL() string {
	if a.cfg.PublicBaseURL == "" {
		return ""
	}
	return a.cfg.PublicBaseURL + "/app/"
}

// authConfig maps the process configuration to the identity service.
func authConfig(cfg config.Config) auth.Config {
	return auth.Config{
		PublicBaseURL: cfg.PublicBaseURL,
		OIDC: auth.OIDCConfig{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURL,
		},
		TelegramBotToken: cfg.Telegram.BotToken,
	}
}

// Handler returns the root HTTP handler. Tests use it without opening a
// listening socket.
func (a *App) Handler() http.Handler { return a.handler }

// Store returns the database store.
func (a *App) Store() *store.Store { return a.store }

// Auth returns the identity service.
func (a *App) Auth() *auth.Service { return a.auth }

// Run starts the background jobs, the messenger transports and the HTTP server,
// then blocks until ctx is cancelled or the server fails. Shutdown waits up to
// shutdownTimeout for in-flight requests.
func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if a.runtime != nil {
		if err := a.runtime.Start(runCtx); err != nil {
			a.logger.Error("messenger runtime failed to start", slog.String("error", err.Error()))
			cancel()
			a.store.Close()
			return fmt.Errorf("start messenger runtime: %w", err)
		}
		a.logger.Info("messenger runtime started")
	}

	jobs := append(append([]Job{}, a.registry.Jobs()...), a.extraJobs...)
	a.jobs.Start(runCtx, jobs)

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
	if a.runtime != nil {
		if err := a.runtime.Close(); err != nil {
			a.logger.Warn("messenger runtime close failed", slog.String("error", err.Error()))
		}
	}
	a.jobs.Wait()
	return runErr
}

// Close releases the database pool. It is safe to call more than once.
func (a *App) Close() {
	a.closeOnce.Do(func() {
		if a.runtime != nil {
			_ = a.runtime.Close()
		}
		if a.store != nil {
			a.store.Close()
		}
	})
}

// nopWriter discards logger output; it only exists for the defensive nil-logger
// branch in New.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
