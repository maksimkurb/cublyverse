package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
	"github.com/maksimkurb/cublyverse/unibot/auth/oidcflow"
	identitymemory "github.com/maksimkurb/cublyverse/unibot/identity/memory"
	interactionmemory "github.com/maksimkurb/cublyverse/unibot/interaction/memory"
	matrixtransport "github.com/maksimkurb/cublyverse/unibot/platform/matrix"
	telegramtransport "github.com/maksimkurb/cublyverse/unibot/platform/telegram"
	sessionmemory "github.com/maksimkurb/cublyverse/unibot/session/memory"
	"github.com/maksimkurb/cublyverse/unibot/webapp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	identityStore := identitymemory.New()
	interactionStore := interactionmemory.New()
	sessionStore := sessionmemory.New[demoSession]()

	var transports []unibot.Transport
	var matrixAdapter *matrixtransport.Adapter

	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		tg, err := telegramtransport.New(telegramtransport.Config{Token: token}, interactionStore)
		must(err)
		transports = append(transports, tg)
	}

	if homeserver := os.Getenv("MATRIX_HOMESERVER"); homeserver != "" {
		mx, err := matrixtransport.New(matrixtransport.Config{
			Homeserver:   homeserver,
			Username:     mustEnv("MATRIX_USERNAME"),
			Password:     mustEnv("MATRIX_PASSWORD"),
			CryptoDBPath: envOr("MATRIX_CRYPTO_DB", "matrix-crypto.db"),
			PickleKey:    []byte(mustEnv("MATRIX_PICKLE_KEY")),
		}, interactionStore)
		must(err)
		matrixAdapter = mx
		transports = append(transports, mx)
	}

	if len(transports) == 0 {
		log.Fatal("configure TELEGRAM_BOT_TOKEN and/or MATRIX_HOMESERVER")
	}

	publicBaseURL := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	webAppURL := publicBaseURL + "/app/"

	var authService *oidcflow.Service
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		redirectURL := os.Getenv("OIDC_REDIRECT_URL")
		if redirectURL == "" && publicBaseURL != "" {
			redirectURL = publicBaseURL + "/auth/callback"
		}
		service, err := oidcflow.New(ctx, oidcflow.Config{
			Issuer:        issuer,
			ClientID:      mustEnv("OIDC_CLIENT_ID"),
			ClientSecret:  os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:   redirectURL,
			PublicBaseURL: publicBaseURL,
		}, identityStore)
		must(err)
		authService = service
	}

	var runtime *unibot.Runtime
	handler := func(ctx context.Context, botctx *unibot.Context, evt unibot.ResolvedEvent) error {
		if evt.Kind == unibot.EventMessage && evt.Message != nil && evt.Platform == unibot.PlatformMatrix && strings.TrimSpace(evt.Message.Text) == "/widget" {
			if matrixAdapter == nil {
				return nil
			}
			if publicBaseURL == "" {
				_, _ = botctx.Reply(ctx, unibot.Message{Text: "PUBLIC_BASE_URL is not configured."})
				return nil
			}
			widgetURL := publicBaseURL + "/app/#/?widgetId=$matrix_widget_id&userId=$matrix_user_id&roomId=$matrix_room_id"
			err := matrixAdapter.InstallElementWidget(ctx, evt.ConversationID, matrixtransport.WidgetSpec{
				ID:                "unibot-demo",
				Name:              "UniBot Demo",
				URL:               widgetURL,
				WaitForIframeLoad: true,
			})
			if err != nil {
				_, _ = botctx.Reply(ctx, unibot.Message{Text: "Could not install the Element widget: " + err.Error()})
				return nil
			}
			_, _ = botctx.Reply(ctx, unibot.Message{Text: "Element widget installed for this room."})
			return nil
		}

		if !evt.Linked {
			if authService == nil {
				_, _ = botctx.Reply(ctx, unibot.Message{Text: "This messenger identity is not linked. Configure OIDC_* variables to enable SSO linking."})
				return nil
			}
			loginURL, err := authService.BeginURL(unibot.Binding{
				Platform:       evt.Platform,
				RemoteUserID:   evt.RemoteUserID,
				ConversationID: evt.ConversationID,
			})
			if err != nil {
				return err
			}
			_, err = botctx.Reply(ctx, unibot.Message{
				Text: "Sign in once to bind this messenger to your SSO identity.",
				Actions: [][]unibot.Action{{{
					ID:    "login",
					Label: "Sign in",
					Icon:  "🔐",
					Kind:  unibot.ActionOpenApp,
					Style: unibot.ActionStylePrimary,
					URL:   loginURL,
				}}},
			})
			return err
		}

		switch evt.Kind {
		case unibot.EventAction:
			_, err := botctx.Reply(ctx, unibot.Message{Text: "Action received: " + evt.Action.ActionID})
			return err
		case unibot.EventChoice:
			_, err := botctx.Reply(ctx, unibot.Message{Text: fmt.Sprintf("Choice %s: %s", evt.Choice.ChoiceID, strings.Join(evt.Choice.OptionIDs, ", "))})
			return err
		case unibot.EventMessage:
			session, _, err := sessionStore.Load(ctx, evt.PrincipalID)
			if err != nil {
				return err
			}
			session.Turns++
			if err := sessionStore.Save(ctx, evt.PrincipalID, session); err != nil {
				return err
			}
			return sendDemo(ctx, botctx, webAppURL, session.Turns)
		default:
			return nil
		}
	}

	var err error
	runtime, err = unibot.NewRuntime(identityStore, handler, transports...)
	must(err)

	if authService != nil {
		authService.OnLinked = func(ctx context.Context, principal unibot.PrincipalID, binding unibot.Binding) {
			_, err := runtime.SendToAddress(ctx, unibot.Address{
				Platform:       binding.Platform,
				RemoteUserID:   binding.RemoteUserID,
				ConversationID: binding.ConversationID,
			}, unibot.Message{Text: "✅ SSO identity linked. Send any message to open the demo."})
			if err != nil {
				log.Printf("post-link message: %v", err)
			}
		}
	}

	must(runtime.Start(ctx))
	defer runtime.Close()

	mux := http.NewServeMux()
	mux.Handle("/app/", http.StripPrefix("/app/", webapp.Handler()))
	if authService != nil {
		mux.Handle("/auth/", authService.Handler())
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	server := &http.Server{
		Addr:              envOr("HTTP_ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("HTTP listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

type demoSession struct {
	Turns int
}

func sendDemo(ctx context.Context, botctx *unibot.Context, webAppURL string, turn int) error {
	if _, err := botctx.Reply(ctx, unibot.Message{
		Text: fmt.Sprintf("Shared SSO session turn: %d\nSame semantic Action, different renderer:", turn),
		Actions: [][]unibot.Action{{
			{ID: "approve", Label: "Approve", Icon: "✅", Kind: unibot.ActionSubmit, Style: unibot.ActionStyleSuccess},
			{ID: "reject", Label: "Reject", Icon: "❌", Kind: unibot.ActionSubmit, Style: unibot.ActionStyleDanger},
		}},
	}); err != nil {
		return err
	}

	if _, err := botctx.Reply(ctx, unibot.Message{
		Choice: &unibot.Choice{
			ID:            "environment",
			Question:      "Which environment?",
			MaxSelections: 1,
			Disclosed:     true,
			Options: []unibot.ChoiceOption{
				{ID: "prod", Label: "Production"},
				{ID: "stage", Label: "Staging"},
				{ID: "dev", Label: "Development"},
			},
		},
	}); err != nil {
		return err
	}

	if webAppURL != "/app/" && !strings.HasPrefix(webAppURL, "/app/") {
		_, err := botctx.Reply(ctx, unibot.Message{
			Text: "WebApp demo:",
			Actions: [][]unibot.Action{{{
				ID:    "webapp",
				Label: "Open app",
				Icon:  "🧩",
				Kind:  unibot.ActionOpenApp,
				Style: unibot.ActionStylePrimary,
				URL:   webAppURL,
			}}},
		})
		return err
	}
	return nil
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
