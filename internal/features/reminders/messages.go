package reminders

import (
	"strconv"
	"strings"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// ActionDone is the id of the «✅ Выполнено» button. The Router dispatches every
// action whose id starts with "reminders." to the feature; the payload carries
// "<reminder id>:<occurrence id>".
const ActionDone = "reminders.done"

// Message layouts. The chat shows local time of the reminder, with seconds only
// in the completion line, exactly like the n8n workflow did.
const (
	layoutDateTime        = "02.01.2006 15:04"
	layoutDateTimeSeconds = "02.01.2006 15:04:05"
)

// minActionTTL is the shortest lifetime of the «✅ Выполнено» button. The default
// 24 hours of unibot would let a button expire before a slow repeat cycle is
// over, so the scheduler always sets an explicit TTL.
const minActionTTL = 48 * time.Hour

// doneActionTTL is how long the button stays valid: three repeat intervals (the
// user may be away for a while) but never less than 48 hours.
func doneActionTTL(repeatMinutes int) time.Duration {
	ttl := time.Duration(repeatMinutes) * time.Minute * 3
	if ttl < minActionTTL {
		return minActionTTL
	}
	return ttl
}

// doneActionPayload is the opaque payload of the button: both ids, because a
// repeat of the same reminder is another occurrence.
func doneActionPayload(reminderID, occurrenceID string) string {
	return reminderID + ":" + occurrenceID
}

// doneAction is the «✅ Выполнено» button of one occurrence. ownerID restricts
// the button to the SSO account: the runtime drops a press from anybody else, so
// a leaked payload cannot confirm a foreign reminder.
func doneAction(reminder Reminder, ownerID, occurrenceID string) unibot.Action {
	return unibot.Action{
		ID:          ActionDone,
		Label:       "Выполнено",
		Icon:        "✅",
		Kind:        unibot.ActionSubmit,
		Style:       unibot.ActionStyleSuccess,
		Payload:     doneActionPayload(reminder.ID, occurrenceID),
		PrincipalID: unibot.PrincipalID(ownerID),
		TTL:         doneActionTTL(reminder.RepeatMinutes),
	}
}

// reminderMessage is the text of a firing: the reminder itself, a blank line and
// the moment it was due in the reminder's time zone. A repeat adds the number of
// repeats, which starts at 1 for the first repeated message.
func reminderMessage(reminder Reminder, ownerID string, scheduledAt time.Time, repeats int, occurrenceID string) unibot.Message {
	when := formatLocal(scheduledAt, reminder.Timezone, layoutDateTime)
	firstLine := "Первое напоминание: " + when
	repeatLine := ""
	repeatHTML := ""
	if repeats > 0 {
		count := strconv.Itoa(repeats)
		repeatLine = "\nКоличество повторов: " + count
		repeatHTML = "\nКоличество повторов: <b>" + count + "</b>"
	}

	text := reminder.Message + "\n\n" + firstLine + repeatLine
	body := unibot.EscapeHTML(reminder.Message) + "\n\nПервое напоминание: <b>" +
		unibot.EscapeHTML(when) + "</b>" + repeatHTML

	return unibot.Message{
		Text:    text,
		HTML:    body,
		Actions: [][]unibot.Action{{doneAction(reminder, ownerID, occurrenceID)}},
	}
}

// completedMessage is what the delivered message becomes after a confirmation:
// the reminder, when it was completed (with seconds) and how often it had to be
// repeated. The button is gone because there is nothing left to press.
func completedMessage(reminder Reminder, occurrence Occurrence, completedAt time.Time) unibot.Message {
	when := formatLocal(occurrence.ScheduledAt, reminder.Timezone, layoutDateTime)
	done := formatLocal(completedAt, reminder.Timezone, layoutDateTimeSeconds)

	firstLine := "Первое напоминание: " + when
	repeatLine := ""
	repeatHTML := ""
	// repeat_count counts every message of the occurrence including the first
	// one, so the number of repeats is one less.
	if repeats := occurrence.RepeatCount - 1; repeats > 0 {
		count := strconv.Itoa(repeats)
		repeatLine = "\nКоличество повторов: " + count
		repeatHTML = "\nКоличество повторов: <b>" + count + "</b>"
	}

	text := reminder.Message + "\n\n✅ Выполнено: " + done + "\n" + firstLine + repeatLine
	body := unibot.EscapeHTML(reminder.Message) + "\n\n✅ Выполнено: <b>" +
		unibot.EscapeHTML(done) + "</b>\nПервое напоминание: <b>" +
		unibot.EscapeHTML(when) + "</b>" + repeatHTML

	return unibot.Message{Text: text, HTML: body}
}

// formatLocal renders an instant in the reminder's time zone. A broken zone is
// a stored-data problem; UTC keeps the message readable instead of failing the
// delivery.
func formatLocal(instant time.Time, timezone, layout string) string {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.UTC
	}
	return instant.In(location).Format(layout)
}

// scheduleSummary describes the days of a rule in one short Russian phrase.
func scheduleSummary(reminder Reminder) string {
	if reminder.Kind == Monthly {
		days := make([]string, 0, len(reminder.Days))
		for _, day := range reminder.Days {
			days = append(days, strconv.Itoa(day))
		}
		return strings.Join(days, ", ") + " числа"
	}
	names := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}
	days := make([]string, 0, len(reminder.Days))
	for _, day := range reminder.Days {
		if day >= 1 && day <= 7 {
			days = append(days, names[day-1])
		}
	}
	return strings.Join(days, ", ")
}
