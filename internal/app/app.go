// Package app assembles the process: configuration, database, HTTP server,
// features and background jobs.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/maksimkurb/cublyverse/api"
	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	"github.com/maksimkurb/cublyverse/internal/interaction/postgres"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
	"github.com/maksimkurb/cublyverse/internal/oauthserver"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/version"
	"github.com/maksimkurb/cublyverse/internal/workspace"
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
	router  *bot.Router
	mcp     *mcpserver.Server

	// delivery is the reminders messenger adapter; it is attached to the
	// runtime in startBot.
	delivery *messengerDelivery

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

	// The reminders service is shared by every entry point: the HTTP API, the
	// bot commands and the scheduler. The delivery adapter is attached to the
	// messenger runtime in startBot.
	delivery := newMessengerDelivery(logger)
	reminderService := reminders.New(st,
		reminders.WithLogger(logger),
		reminders.WithDelivery(delivery),
	)

	// Reminders of the old n8n bot wait for a Telegram binding; the identity
	// service hands over the waiting rows when that binding appears. The claim
	// runs after the binding is committed and is best effort, so an import
	// problem never blocks a sign-in.
	authService.Legacy = reminderService

	workspaces := workspace.New(st)
	craftService := craft.New(st, workspaces)
	craftOpts := []craft.Option{}
	if cfg.Craft.CatalogSync {
		craftOpts = append(craftOpts, craft.WithCatalogSync(catalog.NewSyncer(st, logger), logger))
		logger.Info("craft catalogue sync enabled: official sources refresh weekly")
	}

	registry := NewRegistry(craft.NewFeature(craftService, craftOpts...), reminders.NewFeature(reminderService, logger,
		reminders.WithTickInterval(cfg.RemindersTickIntervalOr()),
		reminders.WithTimezoneLookup(func(ctx context.Context, principalID string) (string, error) {
			profile, err := authService.Profile(ctx, principalID)
			if err != nil {
				return "", err
			}
			return profile.User.Timezone, nil
		}),
	))
	logger.Info("features registered", slog.Any("features", registry.Names()))

	// The MCP endpoint is PAT only: the token resolves to the same principal the
	// HTTP API and the bot use, and the features register their tools through
	// the registry.
	// OAuth for MCP clients needs the public URL: it is the issuer and the root
	// of every resource identifier. Without it only personal tokens work.
	var oauth *oauthserver.Service
	mcpDeps := mcpserver.Deps{
		Version: version.String(),
		Logger:  logger,
		Profile: authService.Profile,
		Authenticate: func(ctx context.Context, r *http.Request) (auth.Session, bool) {
			return authService.ResolveAPIToken(ctx, auth.BearerToken(r), r)
		},
		Spaces: spacesOf(workspaces),
	}
	if cfg.PublicBaseURL != "" {
		oauth = oauthserver.New(oauthserver.Config{Issuer: cfg.PublicBaseURL, Logger: logger}, st, authService)
		mcpDeps.OAuth = oauth
	} else {
		logger.Warn("PUBLIC_BASE_URL is empty: MCP OAuth is disabled, personal access tokens only")
	}
	mcpServer := mcpserver.New(mcpDeps)
	registry.RegisterMCP(mcpServer)

	interactions := postgres.New(st)

	application := &App{
		cfg:      cfg,
		logger:   logger,
		store:    st,
		registry: registry,
		jobs:     NewRunner(logger),
		auth:     authService,
		mcp:      mcpServer,
		delivery: delivery,
	}

	var oauthHandler http.Handler
	var connections httpapi.ConnectionService
	if oauth != nil {
		oauthHandler = oauth.Handler()
		connections = oauth
	}
	handler := httpapi.New(httpapi.Deps{
		OAuth:       oauthHandler,
		Connections: connections,
		Version:     version.String(),
		Logger:      logger,
		Ready:       st.Ping,
		Web:         webassets.Handler(),
		Spec:        api.OpenAPISpec,
		Auth:        authService,
		AuthPages:   authService.Handler(),
		Reminders:   reminderService,
		Workspaces:  workspaces,
		Craft:       craftService,
		MCP:         mcpServer.Handler(),
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
	if oauth != nil {
		application.extraJobs = append(application.extraJobs, oauth.CleanupJob())
	}

	return application, nil
}

// startBot wires the messenger runtime when a transport and OIDC are
// configured: the bot router (core commands plus features), the transports and
// the runtime that orders and bounds event handling.
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

	// The router is the single messenger entry point: core commands plus
	// everything the features registered. It exists before the transports so
	// the Telegram adapter can publish the command menu on start.
	router := bot.NewRouter(
		bot.WithLogger(a.logger),
		bot.WithLoginLinks(a.auth),
		bot.WithWebAppURL(a.webAppURL()),
	)
	a.registry.RegisterBot(router)
	a.router = router

	transports, err := a.transports(interactions, router)
	if err != nil {
		return err
	}
	if len(transports) == 0 {
		return nil
	}

	runtime, err := unibot.NewRuntimeWithConfig(unibot.RuntimeConfig{
		Logger:         a.logger,
		HandlerTimeout: a.cfg.BotHandlerTimeoutOr(),
	}, a.auth, router.Handle, transports...)
	if err != nil {
		return fmt.Errorf("build messenger runtime: %w", err)
	}
	a.runtime = runtime
	// From now on reminders can reach the user; before this point a send fails
	// with a clear error and the scheduler retries on the next tick.
	a.delivery.attach(runtime)

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

	// Tell the user that the reminders of the old n8n bot are back. The
	// reminders already exist; the message is best effort.
	a.auth.OnLegacyImported = func(ctx context.Context, binding unibot.Binding, count int) {
		message := unibot.Message{Text: fmt.Sprintf("Импортировано напоминаний из старого бота: %d", count)}
		if _, err := runtime.SendToAddress(ctx, unibot.Address{
			Platform:       binding.Platform,
			RemoteUserID:   binding.RemoteUserID,
			ConversationID: binding.ConversationID,
		}, message); err != nil {
			a.logger.Warn("post-import message failed", "platform", string(binding.Platform), "error", err)
		}
	}
	return nil
}

// transports builds the messenger adapters that are configured.
func (a *App) transports(interactions unibot.InteractionStore, router *bot.Router) ([]unibot.Transport, error) {
	var transports []unibot.Transport
	if a.cfg.TelegramEnabled() {
		// Telegram accepts web_app menu buttons only over https, so the menu is
		// published only for a public https URL (local development is http).
		menuURL := ""
		if strings.HasPrefix(a.cfg.PublicBaseURL, "https://") {
			menuURL = a.webAppURL()
		}
		adapter, err := telegramtransport.New(telegramtransport.Config{
			Token:     a.cfg.Telegram.BotToken,
			Logger:    a.logger,
			Commands:  telegramCommands(router.Commands()),
			WebAppURL: menuURL,
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

// telegramCommands maps the router commands to the Telegram command menu.
func telegramCommands(commands []bot.CommandInfo) []telegramtransport.Command {
	out := make([]telegramtransport.Command, 0, len(commands))
	for _, command := range commands {
		out = append(out, telegramtransport.Command{
			Command:     command.Name,
			Description: command.Description,
		})
	}
	return out
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

// MCP returns the MCP server. Tests use it with the in-memory transport.
func (a *App) MCP() *mcpserver.Server { return a.mcp }

// Router returns the bot router, or nil when no messenger transport is
// configured.
func (a *App) Router() *bot.Router { return a.router }

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
