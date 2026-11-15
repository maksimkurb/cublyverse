// Package bot routes messenger commands and actions to feature handlers.
//
// The router itself arrives in M2. This file holds the single handler M1 needs:
// an account without a binding gets a «Войти» button that starts the safe SSO
// linking flow. Keeping it tiny makes the M2 router an obvious replacement.
package bot

import (
	"context"
	"log/slog"
	"strings"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// LoginLinks starts a messenger binding flow and returns the URL of the sign-in
// page. *auth.Service implements it.
type LoginLinks interface {
	BeginMessengerBinding(ctx context.Context, platform unibot.Platform, remoteUserID, conversationID string) (string, error)
}

// Messages of the minimal handler. They are user facing, so they are Russian.
const (
	loginText   = "Здравствуйте! Cublyverse работает от имени вашего SSO-аккаунта.\nНажмите «Войти», чтобы привязать этот аккаунт мессенджера."
	loginFailed = "Не удалось создать ссылку для входа. Попробуйте позже."
	linkedText  = "Вы уже вошли. Откройте приложение, чтобы продолжить."
)

// Handler is the minimal M1 bot handler.
type Handler struct {
	// Links creates the sign-in link of an unlinked account.
	Links LoginLinks
	// WebAppURL is the absolute URL of the WebApp, used for the
	// «Открыть приложение» button. An empty value hides the button.
	WebAppURL string
	// Logger records failures; nil falls back to slog.Default.
	Logger *slog.Logger
}

// Handle answers one resolved messenger event.
func (h *Handler) Handle(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent) error {
	if h.Logger == nil {
		h.Logger = slog.Default()
	}
	if !event.Linked {
		return h.sendLoginButton(ctx, botctx, event)
	}
	return h.sendAppButton(ctx, botctx)
}

// sendLoginButton replies with the «Войти» OpenApp button. The flow it starts
// does not bind anything yet: the user still has to confirm the SSO account.
func (h *Handler) sendLoginButton(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent) error {
	if h.Links == nil {
		h.Logger.ErrorContext(ctx, "bot has no login linker")
		_, err := botctx.Reply(ctx, unibot.Message{Text: loginFailed})
		return err
	}
	linkURL, err := h.Links.BeginMessengerBinding(ctx, event.Platform, event.RemoteUserID, event.ConversationID)
	if err != nil {
		h.Logger.ErrorContext(ctx, "create login link failed", "error", err)
		_, replyErr := botctx.Reply(ctx, unibot.Message{Text: loginFailed})
		return replyErr
	}
	_, err = botctx.Reply(ctx, unibot.Message{
		Text: loginText,
		Actions: [][]unibot.Action{{{
			ID:    "login",
			Label: "Войти",
			Icon:  "🔐",
			Kind:  unibot.ActionOpenApp,
			Style: unibot.ActionStylePrimary,
			URL:   linkURL,
		}}},
	})
	return err
}

// sendAppButton answers a linked account. M2 replaces this with real commands.
func (h *Handler) sendAppButton(ctx context.Context, botctx *unibot.Context) error {
	message := unibot.Message{Text: linkedText}
	if url := strings.TrimSpace(h.WebAppURL); url != "" {
		message.Actions = [][]unibot.Action{{{
			ID:    "open_app",
			Label: "Открыть приложение",
			Icon:  "🧩",
			Kind:  unibot.ActionOpenApp,
			Style: unibot.ActionStylePrimary,
			URL:   url,
		}}}
	}
	_, err := botctx.Reply(ctx, message)
	return err
}
