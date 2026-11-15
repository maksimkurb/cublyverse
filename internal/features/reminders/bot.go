package reminders

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/maksimkurb/cublyverse/internal/bot"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Answers of the «✅ Выполнено» button. They are shown as a toast (or as a modal
// dialog for the last one) and are part of the behaviour carried over from the
// n8n workflow, so the texts are fixed.
const (
	ackCompleted   = "Подтверждено ✅"
	ackAlready     = "Уже подтверждено ✅"
	ackStale       = "Это уже неактуальное напоминание"
	ackNotOwner    = "Это напоминание не для вас"
	ackUnavailable = "Напоминание уже недоступно"
	ackFailed      = "Не удалось подтвердить напоминание. Попробуйте позже."
)

// /reminders output limits: a chat message must stay readable, so the list is
// capped and the tail is counted instead of listed.
const (
	maxListedReminders    = 20
	maxListedMessageRunes = 60
	listEmptyText         = "Напоминаний пока нет. Создайте первое в приложении."
	listHeaderText        = "Ваши напоминания:"
	listFailedText        = "Не удалось загрузить напоминания. Попробуйте позже."
)

// registerBot adds /reminders and the reminders.* action prefix to the router.
func (f *Feature) registerBot(router *bot.Router) {
	router.Command("reminders", "список напоминаний", func(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent) error {
		return f.handleList(ctx, botctx, event, router)
	})
	// The prefix is "reminders." so a future action of the feature is routed to
	// one place; the handler dispatches on the exact action id.
	router.Action("reminders.", f.handleAction)
}

// handleAction answers a pressed reminder button.
func (f *Feature) handleAction(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent) error {
	if event.Action == nil {
		return nil
	}
	if event.Action.ActionID != ActionDone {
		return ack(ctx, botctx, ackStale, false)
	}
	reminderID, occurrenceID, ok := splitDonePayload(event.Action.Payload)
	if !ok {
		return ack(ctx, botctx, ackUnavailable, false)
	}

	result, err := f.service.CompleteOccurrence(ctx, string(event.PrincipalID), reminderID, occurrenceID)
	if err != nil {
		f.logger.ErrorContext(ctx, "confirm reminder failed",
			slog.String("reminder_id", reminderID),
			slog.String("occurrence_id", occurrenceID),
			slog.String("error", err.Error()),
		)
		return ack(ctx, botctx, ackFailed, false)
	}
	text, alert := completionAnswer(result)
	return ack(ctx, botctx, text, alert)
}

// handleList answers /reminders with a short list of the user's reminders and,
// when the WebApp is published over https, the «Открыть приложение» button.
func (f *Feature) handleList(ctx context.Context, botctx *unibot.Context, event unibot.ResolvedEvent, router *bot.Router) error {
	items, err := f.service.List(ctx, string(event.PrincipalID))
	if err != nil {
		f.logger.ErrorContext(ctx, "list reminders for the bot failed", slog.String("error", err.Error()))
		_, replyErr := botctx.Reply(ctx, unibot.Message{Text: listFailedText})
		return replyErr
	}

	message := unibot.Message{Text: listText(items)}
	if action, ok := router.AppAction(); ok {
		message.Actions = [][]unibot.Action{{action}}
	}
	_, err = botctx.Reply(ctx, message)
	return err
}

// listText renders the /reminders answer: time, days and state of every rule.
func listText(items []Reminder) string {
	if len(items) == 0 {
		return listEmptyText
	}
	listed := min(len(items), maxListedReminders)

	var text strings.Builder
	text.WriteString(listHeaderText)
	for _, item := range items[:listed] {
		text.WriteString("\n• ")
		text.WriteString(item.TimeHHMM)
		text.WriteString(" ")
		text.WriteString(scheduleSummary(item))
		text.WriteString(" — ")
		text.WriteString(truncateRunes(item.Message, maxListedMessageRunes))
		if !item.Enabled {
			text.WriteString(" (выключено)")
		}
	}
	if rest := len(items) - listed; rest > 0 {
		text.WriteString("\n…и ещё ")
		text.WriteString(strconv.Itoa(rest))
	}
	return text.String()
}

// completionAnswer maps the outcome of a confirmation to the text shown to the
// user. Only "not your reminder" is a modal alert: it means somebody pressed an
// action that does not belong to their account.
func completionAnswer(result CompletionResult) (text string, alert bool) {
	switch result {
	case CompletionCompleted:
		return ackCompleted, false
	case CompletionAlready:
		return ackAlready, false
	case CompletionStale:
		return ackStale, false
	case CompletionNotOwner:
		return ackNotOwner, true
	default:
		return ackUnavailable, false
	}
}

// splitDonePayload parses "<reminder id>:<occurrence id>" out of the button.
func splitDonePayload(payload string) (reminderID, occurrenceID string, ok bool) {
	reminderID, occurrenceID, ok = strings.Cut(payload, ":")
	if !ok || reminderID == "" || occurrenceID == "" {
		return "", "", false
	}
	return reminderID, occurrenceID, true
}

// ack answers an interactive event. A transport without acknowledgement support
// (Matrix) treats it as a no-op.
func ack(ctx context.Context, botctx *unibot.Context, text string, alert bool) error {
	if err := botctx.Ack(ctx, text, alert); err != nil {
		return fmt.Errorf("acknowledge reminder action: %w", err)
	}
	return nil
}

// truncateRunes shortens a message to limit runes, marking the cut with an
// ellipsis.
func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
