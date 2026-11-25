package reminders

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
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

// Russian names used by ScheduleDescription. They are lower case because the
// description is a phrase inside a list line.
var (
	weekdayShortNames = []string{"пн", "вт", "ср", "чт", "пт", "сб", "вс"}
	weekdayFullNames  = []string{"понедельник", "вторник", "среду", "четверг", "пятницу", "субботу", "воскресенье"}
	weekdayGenders    = []string{"m", "m", "f", "m", "f", "f", "n"}
	monthGenitive     = []string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	// Ordinal accusative forms agree with the weekday: «каждый второй
	// вторник», «каждую вторую среду», «каждое второе воскресенье».
	ordinalAccusative = map[string][]string{
		"m": {"первый", "второй", "третий", "четвёртый"},
		"f": {"первую", "вторую", "третью", "четвёртую"},
		"n": {"первое", "второе", "третье", "четвёртое"},
	}
	ordinalNominative = []string{"первый", "второй", "третий", "четвёртый"}
)

// ScheduleDescription is the single human-readable Russian description of a
// schedule. The bot's list and confirmation texts use it; the WebApp mirrors the
// same wording in `web/src/features/reminders/format.ts`, so the chat and the app
// cannot describe one rule differently.
func ScheduleDescription(reminder Reminder) string {
	timeOfDay := reminder.TimeHHMM
	interval := reminder.Interval
	if interval < 1 {
		interval = 1
	}

	var description string
	switch reminder.Kind {
	case Once:
		return fmt.Sprintf("%s в %s (один раз)", formatCivilDate(reminder.StartDate), timeOfDay)

	case Daily:
		description = fmt.Sprintf("%s в %s", everyDays(interval), timeOfDay)

	case Weekly:
		days := make([]string, 0, len(reminder.Days))
		for _, day := range reminder.Days {
			if day >= 1 && day <= 7 {
				days = append(days, weekdayShortNames[day-1])
			}
		}
		if interval == 1 && len(days) == 7 {
			description = fmt.Sprintf("каждый день в %s", timeOfDay)
			break
		}
		description = fmt.Sprintf("%s по %s в %s", everyWeeks(interval), strings.Join(days, ", "), timeOfDay)

	case Monthly:
		description = monthlyDescription(reminder, interval, timeOfDay)

	case Yearly:
		description = fmt.Sprintf("%s %d %s в %s",
			everyYears(interval), reminder.StartDate.Day, monthName(reminder.StartDate.Month), timeOfDay)

	default:
		description = fmt.Sprintf("%s в %s", scheduleSummary(reminder), timeOfDay)
	}

	return description + endDescription(reminder.End)
}

// monthlyDescription renders both monthly forms: a day of month and an nth
// weekday.
func monthlyDescription(reminder Reminder, interval int, timeOfDay string) string {
	if reminder.MonthWeekPosition != 0 {
		weekday := reminder.MonthWeekday
		if weekday < 1 || weekday > 7 {
			return fmt.Sprintf("каждый месяц в %s", timeOfDay)
		}
		gender := weekdayGenders[weekday-1]
		name := weekdayFullNames[weekday-1]
		if interval == 1 {
			ordinal := map[string]string{"m": "последний", "f": "последнюю", "n": "последнее"}[gender]
			if reminder.MonthWeekPosition > 0 {
				ordinal = ordinalAccusative[gender][reminder.MonthWeekPosition-1]
			}
			adverb := map[string]string{"m": "каждый", "f": "каждую", "n": "каждое"}[gender]
			return fmt.Sprintf("%s %s %s месяца в %s", adverb, ordinal, name, timeOfDay)
		}
		ordinal := "последний"
		if reminder.MonthWeekPosition > 0 {
			ordinal = ordinalNominative[reminder.MonthWeekPosition-1]
		}
		return fmt.Sprintf("%s, %s %s в %s", everyMonths(interval), ordinal, name, timeOfDay)
	}

	days := make([]string, 0, len(reminder.Days))
	for _, day := range reminder.Days {
		days = append(days, strconv.Itoa(day))
	}
	joined := joinRussian(days)
	if interval == 1 && len(days) == 1 {
		return fmt.Sprintf("каждое %s-е число месяца в %s", joined, timeOfDay)
	}
	return fmt.Sprintf("%s %s числа в %s", everyMonths(interval), joined, timeOfDay)
}

// endDescription appends the stop condition, if there is one.
func endDescription(end schedule.End) string {
	switch end.Kind {
	case schedule.EndUntil:
		return " до " + formatCivilDate(end.Date)
	case schedule.EndCount:
		return fmt.Sprintf(" (всего %d %s)", end.Count, plural(end.Count, "раз", "раза", "раз"))
	default:
		return ""
	}
}

// everyDays phrases "every day" versus "every N days".
func everyDays(interval int) string {
	if interval == 1 {
		return "каждый день"
	}
	return fmt.Sprintf("каждые %d %s", interval, plural(interval, "день", "дня", "дней"))
}

// everyWeeks phrases "every week" versus "every N weeks".
func everyWeeks(interval int) string {
	if interval == 1 {
		return "каждую неделю"
	}
	return fmt.Sprintf("каждые %d %s", interval, plural(interval, "неделю", "недели", "недель"))
}

// everyMonths phrases "every month" versus "every N months".
func everyMonths(interval int) string {
	if interval == 1 {
		return "каждый месяц"
	}
	return fmt.Sprintf("каждые %d %s", interval, plural(interval, "месяц", "месяца", "месяцев"))
}

// everyYears phrases "every year" versus "every N years".
func everyYears(interval int) string {
	if interval == 1 {
		return "каждый год"
	}
	return fmt.Sprintf("каждые %d %s", interval, plural(interval, "год", "года", "лет"))
}

// plural picks the Russian form for a number: 1 день, 2 дня, 5 дней.
func plural(number int, one, few, many string) string {
	mod100 := number % 100
	if mod100 >= 11 && mod100 <= 14 {
		return many
	}
	switch number % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

// joinRussian joins phrases with commas and "и" before the last one.
func joinRussian(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " и " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " и " + items[len(items)-1]
	}
}

// monthName returns the genitive Russian month name.
func monthName(month time.Month) string {
	if month < time.January || month > time.December {
		return ""
	}
	return monthGenitive[month-1]
}

// formatCivilDate renders a civil date as дд.мм.гггг.
func formatCivilDate(date schedule.Date) string {
	return fmt.Sprintf("%02d.%02d.%04d", date.Day, int(date.Month), date.Year)
}
