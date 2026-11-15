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

// Messages of the built-in handlers. They are user facing, so they are Russian.
const (
	loginText   = "Здравствуйте! Cublyverse работает от имени вашего SSO-аккаунта.\nНажмите «Войти», чтобы привязать этот аккаунт мессенджера."
	loginFailed = "Не удалось создать ссылку для входа. Попробуйте позже."
	linkedText  = "Вы уже вошли. Откройте приложение, чтобы продолжить."
	startText   = "Здравствуйте! Это Cublyverse — личный помощник с напоминаниями.\nСписок команд — /help."
	unknownText = "Неизвестная команда. Список команд: /help."
	staleAction = "Это действие больше неактуально."
)

// Action IDs of the built-in buttons.
const (
	actionLogin   = "login"
	actionOpenApp = "open_app"
)

// registerCoreCommands adds the commands available to every user.
func (r *Router) registerCoreCommands() {
	r.Command("start", "начать работу", r.handleStart)
	r.Command("help", "список команд", r.handleHelp)
}

// handleStart greets the user and offers the WebApp.
func (r *Router) handleStart(ctx context.Context, botctx *unibot.Context, _ unibot.ResolvedEvent) error {
	return r.replyWithAppButton(ctx, botctx, startText)
}

// handleHelp lists every registered command, including the feature ones.
func (r *Router) handleHelp(ctx context.Context, botctx *unibot.Context, _ unibot.ResolvedEvent) error {
	var text strings.Builder
	text.WriteString("Доступные команды:")
	for _, command := range r.commands {
		text.WriteString("\n/")
		text.WriteString(command.Name)
		if command.Description != "" {
			text.WriteString(" — ")
			text.WriteString(command.Description)
		}
	}
	return r.replyWithAppButton(ctx, botctx, text.String())
}

// handleUnlinked answers an account without a binding: the only useful action
// is to sign in, and the flow does not bind anything until the user confirms
// the SSO account on the confirmation page.
func (r *Router) handleUnlinked(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent) error {
	if r.links == nil {
		r.logger.ErrorContext(ctx, "bot has no login linker")
		_, err := botctx.Reply(ctx, unibot.Message{Text: loginFailed})
		return err
	}
	linkURL, err := r.links.BeginMessengerBinding(ctx, event.Platform, event.RemoteUserID, event.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "create login link failed", slog.String("error", err.Error()))
		_, replyErr := botctx.Reply(ctx, unibot.Message{Text: loginFailed})
		return replyErr
	}
	_, err = botctx.Reply(ctx, unibot.Message{
		Text: loginText,
		Actions: [][]unibot.Action{{{
			ID:    actionLogin,
			Label: "Войти",
			Icon:  "🔐",
			Kind:  unibot.ActionOpenApp,
			Style: unibot.ActionStylePrimary,
			URL:   linkURL,
		}}},
	})
	return err
}

// handleFallback answers a linked account that sent something the router does
// not understand: /help is the way out, plus the WebApp button.
func (r *Router) handleFallback(ctx context.Context, botctx *unibot.Context, _ unibot.ResolvedEvent) error {
	return r.replyWithAppButton(ctx, botctx, linkedText)
}

// replyUnknownCommand answers /something that no feature registered.
func (r *Router) replyUnknownCommand(ctx context.Context, botctx *unibot.Context) error {
	_, err := botctx.Reply(ctx, unibot.Message{Text: unknownText})
	return err
}

// replyStaleAction answers a button whose action prefix no longer matches.
func (r *Router) replyStaleAction(ctx context.Context, botctx *unibot.Context) error {
	_, err := botctx.Reply(ctx, unibot.Message{Text: staleAction})
	return err
}

// replyWithAppButton sends text and, when the WebApp is configured, the
// «Открыть приложение» button.
func (r *Router) replyWithAppButton(ctx context.Context, botctx *unibot.Context, text string) error {
	message := unibot.Message{Text: text}
	if r.webAppURL != "" {
		message.Actions = [][]unibot.Action{{{
			ID:    actionOpenApp,
			Label: "Открыть приложение",
			Icon:  "🧩",
			Kind:  unibot.ActionOpenApp,
			Style: unibot.ActionStylePrimary,
			URL:   r.webAppURL,
		}}}
	}
	_, err := botctx.Reply(ctx, message)
	return err
}
