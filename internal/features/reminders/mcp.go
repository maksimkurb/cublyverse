package reminders

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// MCP tool names. They are stable: a client stores them in its configuration.
const (
	toolList     = "reminders_list"
	toolGet      = "reminders_get"
	toolCreate   = "reminders_create"
	toolUpdate   = "reminders_update"
	toolDelete   = "reminders_delete"
	toolComplete = "reminders_complete"
)

// weekdayNames maps ISO weekday numbers to the short names an LLM can write.
var weekdayNames = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// registerMCP adds the reminder tools to the MCP server. Every tool calls the
// same service as the HTTP API and the bot, so business rules never diverge.
func (f *Feature) registerMCP(server *mcpserver.Server) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name:  toolList,
		Title: "List reminders",
		Description: "List every reminder of the current user with its schedule, " +
			"next due time and, when a reminder has fired and waits for " +
			"confirmation, its pending occurrence. Returns an empty array when the " +
			"user has no reminders.",
		Annotations: &mcp.ToolAnnotations{
			Title:         "List reminders",
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}, f.mcpList)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:  toolGet,
		Title: "Get a reminder",
		Description: "Return one reminder by id together with its pending occurrence, " +
			"if any. A reminder of another user is reported as not found.",
		Annotations: &mcp.ToolAnnotations{
			Title:         "Get a reminder",
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}, f.mcpGet)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:  toolCreate,
		Title: "Create a reminder",
		Description: "Create a reminder for the current user. The recurrence is chosen " +
			"with repeat: once (needs date), daily, weekly (needs weekdays), monthly " +
			"(month_days or nth_weekday such as the second Tuesday or the last Friday) " +
			"or yearly. interval makes it every N days/weeks/months/years and is " +
			"relative to start_date; until or count stops the repetition. The old " +
			"shape without repeat still works: weekdays means weekly and month_days " +
			"means monthly. Without a time zone the user's profile time zone is " +
			"used. Returns the stored reminder with its next due time in UTC.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create a reminder",
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}, f.mcpCreate)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:  toolUpdate,
		Title: "Update a reminder",
		Description: "Change a reminder. Omitted fields keep their current values: " +
			"message, repeat and its interval/weekdays/month_days/nth_weekday/date/" +
			"start_date/until/count, time, time_zone, repeat_minutes and enabled are " +
			"all optional apart from the id. A recurrence change cancels the pending " +
			"occurrence, resets the fired counter and recomputes the next due time. " +
			"Returns the updated reminder.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update a reminder",
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, f.mcpUpdate)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:  toolDelete,
		Title: "Delete a reminder",
		Description: "Delete a reminder and its occurrence history. Deleting an " +
			"already deleted reminder is reported as not found.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete a reminder",
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, f.mcpDelete)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:  toolComplete,
		Title: "Complete a reminder occurrence",
		Description: "Confirm the occurrence that is waiting for confirmation, exactly " +
			"like the «✅ Выполнено» button in the chat. Repeats stop and the " +
			"delivered chat message is updated. If the reminder has not fired, was " +
			"already completed or was edited, the call is a tool error: there is " +
			"nothing to confirm.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Complete a reminder occurrence",
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, f.mcpComplete)
}

func boolPtr(value bool) *bool { return &value }

// mcpListInput has no arguments: the user comes from the token.
type mcpListInput struct{}

// mcpGetInput identifies one reminder.
type mcpGetInput struct {
	// ID is the reminder UUID.
	ID string `json:"id" jsonschema:"reminder id (UUID) returned by reminders_list or reminders_create"`
}

// mcpCreateInput is the LLM-friendly form of a new reminder. The recurrence is
// described by `repeat` plus the fields of that kind; the old shape (weekdays or
// month_days without `repeat`) still works and means weekly or monthly.
type mcpCreateInput struct {
	// Message is the text the user receives.
	Message string `json:"message" jsonschema:"reminder text in the user's language, 1 to 1500 characters"`
	// Repeat is the recurrence kind.
	Repeat string `json:"repeat,omitempty" jsonschema:"recurrence kind: once, daily, weekly, monthly or yearly. Omit it to use the old shape: weekdays means weekly, month_days means monthly"`
	// Interval is the period in repeat units.
	Interval int `json:"interval,omitempty" jsonschema:"every N days/weeks/months/years, 1..1000. Default 1, for example interval=2 with repeat=weekly means every second week"`
	// Weekdays are the days of a weekly schedule.
	Weekdays []string `json:"weekdays,omitempty" jsonschema:"weekly schedule: day names mon,tue,wed,thu,fri,sat,sun or ISO numbers 1..7 (1 = Monday). Use either weekdays, month_days or nth_weekday"`
	// MonthDays are the days of a monthly day-of-month schedule.
	MonthDays []int `json:"month_days,omitempty" jsonschema:"monthly day-of-month schedule: days 1..31. A day the month does not have means its last day. Use either weekdays, month_days or nth_weekday"`
	// NthWeekday is the nth weekday of a monthly schedule.
	NthWeekday *mcpNthWeekday `json:"nth_weekday,omitempty" jsonschema:"monthly nth-weekday schedule, for example the second Tuesday. Use either weekdays, month_days or nth_weekday"`
	// Date is the day of a one-time reminder.
	Date string `json:"date,omitempty" jsonschema:"date of a one-time reminder as YYYY-MM-DD in the reminder time zone. Also accepted as start_date for repeat=once"`
	// StartDate is the first possible day of a repeating schedule.
	StartDate string `json:"start_date,omitempty" jsonschema:"first possible day as YYYY-MM-DD in the reminder time zone; defaults to today. For repeat=yearly its month and day are the anniversary"`
	// Until is the last allowed day of a repeating schedule.
	Until string `json:"until,omitempty" jsonschema:"last allowed day as YYYY-MM-DD; the reminder stops after it. Do not combine with count"`
	// Count is the total number of occurrences of a repeating schedule.
	Count int `json:"count,omitempty" jsonschema:"total number of occurrences, 1..10000; the reminder stops after them. Do not combine with until"`
	// Time is the local wall clock time.
	Time string `json:"time" jsonschema:"local wall clock time as HH:MM in 24-hour form, for example 09:30"`
	// Timezone is the IANA time zone of the schedule.
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA time zone such as Europe/Moscow. Defaults to the user's profile time zone"`
	// RepeatMinutes is the repeat interval of an unconfirmed occurrence.
	RepeatMinutes *int `json:"repeat_minutes,omitempty" jsonschema:"how often an unconfirmed occurrence repeats, 1..1440 minutes. Default 10"`
	// Enabled creates the reminder already scheduled, or disabled.
	Enabled *bool `json:"enabled,omitempty" jsonschema:"create the reminder already scheduled. Default true"`
}

// mcpNthWeekday selects the nth weekday of a month.
type mcpNthWeekday struct {
	// Position is 1..4, or -1 for the last weekday.
	Position int `json:"position" jsonschema:"position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last"`
	// Weekday is the weekday name or ISO number.
	Weekday string `json:"weekday" jsonschema:"weekday name mon..sun or ISO number 1..7 (1 = Monday)"`
}

// mcpUpdateInput is a partial update: omitted fields keep their current value.
type mcpUpdateInput struct {
	// ID is the reminder UUID.
	ID string `json:"id" jsonschema:"reminder id (UUID) to change"`
	// Message replaces the text.
	Message string `json:"message,omitempty" jsonschema:"new reminder text, 1 to 1500 characters. Omit to keep the current text"`
	// Repeat replaces the recurrence kind.
	Repeat string `json:"repeat,omitempty" jsonschema:"new recurrence kind: once, daily, weekly, monthly or yearly. Omit to keep the current one"`
	// Interval replaces the period.
	Interval *int `json:"interval,omitempty" jsonschema:"every N days/weeks/months/years, 1..1000. Omit to keep the current interval"`
	// Weekdays replaces a weekly schedule.
	Weekdays []string `json:"weekdays,omitempty" jsonschema:"weekly schedule: day names mon..sun or ISO numbers 1..7. Omit to keep the current schedule"`
	// MonthDays replaces a monthly day-of-month schedule.
	MonthDays []int `json:"month_days,omitempty" jsonschema:"monthly day-of-month schedule: days 1..31. Omit to keep the current schedule"`
	// NthWeekday replaces a monthly schedule with the nth weekday.
	NthWeekday *mcpNthWeekday `json:"nth_weekday,omitempty" jsonschema:"monthly nth-weekday schedule. Omit to keep the current schedule"`
	// Date replaces the day of a one-time reminder.
	Date string `json:"date,omitempty" jsonschema:"new date of a one-time reminder as YYYY-MM-DD. Omit to keep the current date"`
	// StartDate replaces the first possible day.
	StartDate string `json:"start_date,omitempty" jsonschema:"new first possible day as YYYY-MM-DD. Omit to keep the current one"`
	// Until replaces the last allowed day.
	Until string `json:"until,omitempty" jsonschema:"new last allowed day as YYYY-MM-DD; removes a count end. Omit to keep the current end"`
	// Count replaces the total number of occurrences.
	Count *int `json:"count,omitempty" jsonschema:"new total number of occurrences 1..10000; removes an until end. Omit to keep the current end"`
	// Time replaces the local wall clock time.
	Time string `json:"time,omitempty" jsonschema:"new local wall clock time HH:MM. Omit to keep the current time"`
	// Timezone replaces the IANA time zone.
	Timezone string `json:"timezone,omitempty" jsonschema:"new IANA time zone. Omit to keep the current one"`
	// RepeatMinutes replaces the repeat interval.
	RepeatMinutes *int `json:"repeat_minutes,omitempty" jsonschema:"new repeat interval 1..1440 minutes. Omit to keep the current one"`
	// Enabled enables or disables the reminder.
	Enabled *bool `json:"enabled,omitempty" jsonschema:"enable or disable the reminder. Omit to keep the current state"`
}

// mcpDeleteInput identifies one reminder.
type mcpDeleteInput struct {
	// ID is the reminder UUID.
	ID string `json:"id" jsonschema:"reminder id (UUID) to delete"`
}

// mcpCompleteInput identifies the reminder whose occurrence is confirmed.
type mcpCompleteInput struct {
	// ID is the reminder UUID.
	ID string `json:"id" jsonschema:"reminder id (UUID) whose pending occurrence is confirmed"`
}

// mcpListOutput is the answer of reminders_list.
type mcpListOutput struct {
	// Reminders are all reminders of the user.
	Reminders []mcpReminder `json:"reminders" jsonschema:"every reminder of the user; empty when there are none"`
}

// mcpDeleteOutput is the answer of reminders_delete.
type mcpDeleteOutput struct {
	// ID is the deleted reminder.
	ID string `json:"id" jsonschema:"id of the deleted reminder"`
	// Deleted reports that the reminder is gone.
	Deleted bool `json:"deleted" jsonschema:"always true on success"`
}

// mcpReminder is the MCP representation of a reminder. Times are RFC 3339
// strings in UTC; the schedule itself is local to `time_zone`.
type mcpReminder struct {
	// ID is the reminder UUID.
	ID string `json:"id" jsonschema:"reminder id (UUID)"`
	// Message is the reminder text.
	Message string `json:"message" jsonschema:"reminder text"`
	// Repeat is the recurrence kind.
	Repeat string `json:"repeat" jsonschema:"once, daily, weekly, monthly or yearly"`
	// ScheduleKind is kept for clients of the first schema: the same value as
	// repeat.
	ScheduleKind string `json:"schedule_kind" jsonschema:"alias of repeat, kept for older clients"`
	// Interval is the period in repeat units.
	Interval int `json:"interval" jsonschema:"every N days/weeks/months/years"`
	// Weekdays are the weekly days as names, present for a weekly schedule.
	Weekdays []string `json:"weekdays,omitempty" jsonschema:"weekly days as mon..sun, present when repeat is weekly"`
	// MonthDays are the monthly days, present for a day-of-month schedule.
	MonthDays []int `json:"month_days,omitempty" jsonschema:"days of month, present when repeat is monthly and nth_weekday is absent"`
	// NthWeekday is the nth weekday, present for an nth-weekday monthly schedule.
	NthWeekday *mcpNthWeekday `json:"nth_weekday,omitempty" jsonschema:"nth weekday of the month, present when repeat is monthly and month_days is absent"`
	// StartDate is the first possible day of the recurrence.
	StartDate string `json:"start_date" jsonschema:"first possible day as YYYY-MM-DD in the reminder time zone"`
	// Until is the last allowed day, empty when the recurrence does not end by
	// date.
	Until string `json:"until,omitempty" jsonschema:"last allowed day as YYYY-MM-DD, empty when there is no date end"`
	// Count is the total number of occurrences, zero when the recurrence does
	// not end by count.
	Count int `json:"count,omitempty" jsonschema:"total number of occurrences, zero when there is no count end"`
	// OccurrencesFired is how many occurrences already fired.
	OccurrencesFired int `json:"occurrences_fired" jsonschema:"how many occurrences already fired; editing resets it"`
	// Finished reports whether a finite recurrence is over.
	Finished bool `json:"finished" jsonschema:"true when the recurrence has no future occurrence and the last one was confirmed"`
	// FinishedAt is when the recurrence was finished.
	FinishedAt string `json:"finished_at,omitempty" jsonschema:"finish time in RFC 3339 UTC, empty while not finished"`
	// Time is the local wall clock time.
	Time string `json:"time" jsonschema:"local wall clock time HH:MM"`
	// Timezone is the IANA time zone the schedule is interpreted in.
	Timezone string `json:"time_zone" jsonschema:"IANA time zone of the schedule"`
	// RepeatMinutes is the repeat interval of an unconfirmed occurrence.
	RepeatMinutes int `json:"repeat_minutes" jsonschema:"interval in minutes between repeats of an unconfirmed occurrence"`
	// Enabled reports whether the reminder is scheduled.
	Enabled bool `json:"enabled" jsonschema:"whether the reminder is scheduled"`
	// NextDueAt is the next firing in UTC, or empty when disabled.
	NextDueAt string `json:"next_due_at,omitempty" jsonschema:"next firing in RFC 3339 UTC, empty while the reminder is disabled or finished"`
	// LastError is the last delivery error, empty when deliveries work.
	LastError string `json:"last_error,omitempty" jsonschema:"last delivery error, empty when the reminder is delivered"`
	// ActiveOccurrence is the firing that waits for confirmation, if any.
	ActiveOccurrence *mcpOccurrence `json:"active_occurrence,omitempty" jsonschema:"the firing that waits for confirmation, absent otherwise"`
	// CreatedAt is when the reminder was created.
	CreatedAt string `json:"created_at" jsonschema:"creation time in RFC 3339 UTC"`
	// UpdatedAt is when the reminder was last changed.
	UpdatedAt string `json:"updated_at" jsonschema:"last change time in RFC 3339 UTC"`
}

// mcpOccurrence is one firing of a reminder.
type mcpOccurrence struct {
	// ID is the occurrence UUID.
	ID string `json:"id" jsonschema:"occurrence id (UUID)"`
	// Status is active, completed or cancelled.
	Status string `json:"status" jsonschema:"active while waiting for confirmation, completed after confirmation, cancelled when the rule changed"`
	// ScheduledAt is when the occurrence was due.
	ScheduledAt string `json:"scheduled_at" jsonschema:"due time in RFC 3339 UTC"`
	// FirstSentAt is when the first message went out.
	FirstSentAt string `json:"first_sent_at,omitempty" jsonschema:"time of the first delivered message in RFC 3339 UTC"`
	// RepeatCount is how many messages were sent for the occurrence.
	RepeatCount int `json:"repeat_count" jsonschema:"number of delivered messages, including the first one"`
	// NextRepeatAt is when the next repeat is due.
	NextRepeatAt string `json:"next_repeat_at,omitempty" jsonschema:"next repeat time in RFC 3339 UTC, empty when there is none"`
	// CompletedAt is when the user confirmed the occurrence.
	CompletedAt string `json:"completed_at,omitempty" jsonschema:"confirmation time in RFC 3339 UTC"`
}

func (f *Feature) mcpList(ctx context.Context, _ *mcp.CallToolRequest, _ mcpListInput) (*mcp.CallToolResult, mcpListOutput, error) {
	principal, err := f.mcpPrincipal(ctx, auth.ScopeRemindersRead)
	if err != nil {
		return nil, mcpListOutput{}, err
	}
	items, err := f.service.List(ctx, principal.ID)
	if err != nil {
		return nil, mcpListOutput{}, mcpServiceError(err)
	}
	out := mcpListOutput{Reminders: make([]mcpReminder, 0, len(items))}
	for _, item := range items {
		out.Reminders = append(out.Reminders, reminderToMCP(item))
	}
	return nil, out, nil
}

func (f *Feature) mcpGet(ctx context.Context, _ *mcp.CallToolRequest, input mcpGetInput) (*mcp.CallToolResult, mcpReminder, error) {
	principal, err := f.mcpPrincipal(ctx, auth.ScopeRemindersRead)
	if err != nil {
		return nil, mcpReminder{}, err
	}
	reminder, err := f.service.Get(ctx, principal.ID, input.ID)
	if err != nil {
		return nil, mcpReminder{}, mcpServiceError(err)
	}
	return nil, reminderToMCP(reminder), nil
}

func (f *Feature) mcpCreate(ctx context.Context, _ *mcp.CallToolRequest, input mcpCreateInput) (*mcp.CallToolResult, mcpReminder, error) {
	principal, err := f.mcpPrincipal(ctx, auth.ScopeRemindersWrite)
	if err != nil {
		return nil, mcpReminder{}, err
	}
	kind, days, position, weekday, err := parseMCPRecurrence(input.Repeat, input.Weekdays, input.MonthDays, input.NthWeekday)
	if err != nil {
		return nil, mcpReminder{}, err
	}
	startDate := strings.TrimSpace(input.StartDate)
	if date := strings.TrimSpace(input.Date); date != "" {
		if kind != Once {
			return nil, mcpReminder{}, errors.New("date is only for repeat=once; use start_date for a repeating reminder")
		}
		startDate = date
	}
	if kind == Once && startDate == "" {
		return nil, mcpReminder{}, errors.New("repeat=once requires date as YYYY-MM-DD")
	}
	endKind, endDate, endCount, err := parseMCPEnd(input.Until, input.Count)
	if err != nil {
		return nil, mcpReminder{}, err
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = f.defaultTimezone(ctx, principal.ID)
	}
	repeatMinutes := DefaultRepeatMinutes
	if input.RepeatMinutes != nil {
		repeatMinutes = *input.RepeatMinutes
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	reminder, err := f.service.Create(ctx, principal.ID, Input{
		Message:           input.Message,
		Timezone:          timezone,
		Kind:              kind,
		Days:              days,
		Interval:          input.Interval,
		MonthWeekPosition: position,
		MonthWeekday:      weekday,
		StartDate:         startDate,
		EndKind:           endKind,
		EndDate:           endDate,
		EndCount:          endCount,
		TimeHHMM:          input.Time,
		RepeatMinutes:     repeatMinutes,
		Enabled:           enabled,
	})
	if err != nil {
		return nil, mcpReminder{}, mcpServiceError(err)
	}
	return nil, reminderToMCP(reminder), nil
}

func (f *Feature) mcpUpdate(ctx context.Context, _ *mcp.CallToolRequest, input mcpUpdateInput) (*mcp.CallToolResult, mcpReminder, error) {
	principal, err := f.mcpPrincipal(ctx, auth.ScopeRemindersWrite)
	if err != nil {
		return nil, mcpReminder{}, err
	}
	// The service replaces the whole rule, so the tool reads the current one and
	// merges the supplied fields. The read is what makes an update partial from
	// the client's point of view without duplicating the rule in the adapter.
	current, err := f.service.Get(ctx, principal.ID, input.ID)
	if err != nil {
		return nil, mcpReminder{}, mcpServiceError(err)
	}
	rule := inputFromReminder(current)

	// A shape change is recognised by any recurrence field. Without one the
	// current schedule is kept, so an update of the text does not touch it.
	if input.Repeat != "" || len(input.Weekdays) > 0 || len(input.MonthDays) > 0 || input.NthWeekday != nil {
		repeat := input.Repeat
		if repeat == "" {
			// The old shape: weekdays or month_days alone imply the kind.
			switch {
			case input.NthWeekday != nil:
				repeat = string(Monthly)
			case len(input.MonthDays) > 0:
				repeat = string(Monthly)
			case len(input.Weekdays) > 0:
				repeat = string(Weekly)
			default:
				repeat = string(current.Kind)
			}
		}
		kind, days, position, weekday, err := parseMCPRecurrence(repeat, input.Weekdays, input.MonthDays, input.NthWeekday)
		if err != nil {
			return nil, mcpReminder{}, err
		}
		rule.Kind = kind
		rule.Days = days
		rule.MonthWeekPosition = position
		rule.MonthWeekday = weekday
	}
	if input.Interval != nil {
		rule.Interval = *input.Interval
	}
	if date := strings.TrimSpace(input.Date); date != "" {
		if rule.Kind != Once {
			return nil, mcpReminder{}, errors.New("date is only for repeat=once; use start_date for a repeating reminder")
		}
		rule.StartDate = date
	}
	if startDate := strings.TrimSpace(input.StartDate); startDate != "" {
		rule.StartDate = startDate
	}
	if input.Until != "" || input.Count != nil {
		endKind, endDate, endCount, err := parseMCPEnd(input.Until, valueOrZero(input.Count))
		if err != nil {
			return nil, mcpReminder{}, err
		}
		rule.EndKind, rule.EndDate, rule.EndCount = endKind, endDate, endCount
	}

	if strings.TrimSpace(input.Message) != "" {
		rule.Message = input.Message
	}
	if strings.TrimSpace(input.Time) != "" {
		rule.TimeHHMM = input.Time
	}
	if strings.TrimSpace(input.Timezone) != "" {
		rule.Timezone = input.Timezone
	}
	if input.RepeatMinutes != nil {
		rule.RepeatMinutes = *input.RepeatMinutes
	}
	if input.Enabled != nil {
		rule.Enabled = *input.Enabled
	}

	reminder, err := f.service.Update(ctx, principal.ID, input.ID, rule)
	if err != nil {
		return nil, mcpReminder{}, mcpServiceError(err)
	}
	return nil, reminderToMCP(reminder), nil
}

func (f *Feature) mcpDelete(ctx context.Context, _ *mcp.CallToolRequest, input mcpDeleteInput) (*mcp.CallToolResult, mcpDeleteOutput, error) {
	principal, err := f.mcpPrincipal(ctx, auth.ScopeRemindersWrite)
	if err != nil {
		return nil, mcpDeleteOutput{}, err
	}
	if err := f.service.Delete(ctx, principal.ID, input.ID); err != nil {
		return nil, mcpDeleteOutput{}, mcpServiceError(err)
	}
	return nil, mcpDeleteOutput{ID: input.ID, Deleted: true}, nil
}

func (f *Feature) mcpComplete(ctx context.Context, _ *mcp.CallToolRequest, input mcpCompleteInput) (*mcp.CallToolResult, mcpReminder, error) {
	principal, err := f.mcpPrincipal(ctx, auth.ScopeRemindersWrite)
	if err != nil {
		return nil, mcpReminder{}, err
	}
	reminder, err := f.service.Complete(ctx, principal.ID, input.ID)
	if err != nil {
		return nil, mcpReminder{}, mcpServiceError(err)
	}
	return nil, reminderToMCP(reminder), nil
}

// mcpPrincipal resolves the caller and checks the scope of the tool. Tool
// errors are ordinary results (isError), so the model can read them.
func (f *Feature) mcpPrincipal(ctx context.Context, scope string) (mcpserver.Principal, error) {
	principal, ok := mcpserver.PrincipalFromContext(ctx)
	if !ok {
		return mcpserver.Principal{}, mcpserver.ErrUnauthenticated
	}
	if err := mcpserver.RequireScope(principal, scope); err != nil {
		return mcpserver.Principal{}, err
	}
	return principal, nil
}

// defaultTimezone returns the profile time zone of the user, or UTC when the
// lookup is not wired. It is the default for a new reminder, so the model does
// not have to ask for the time zone in the common case.
func (f *Feature) defaultTimezone(ctx context.Context, principalID string) string {
	if f.timezone == nil {
		return "UTC"
	}
	timezone, err := f.timezone(ctx, principalID)
	if err != nil || strings.TrimSpace(timezone) == "" {
		return "UTC"
	}
	return timezone
}

// parseMCPRecurrence turns the LLM-facing recurrence fields into the service
// form. An empty repeat keeps the old shape: weekdays means weekly, month_days
// means monthly, nth_weekday means a monthly nth weekday.
func parseMCPRecurrence(repeat string, weekdays []string, monthDays []int, nth *mcpNthWeekday) (Kind, []int, int, int, error) {
	kind := Kind(strings.ToLower(strings.TrimSpace(repeat)))
	if kind == "" {
		switch {
		case nth != nil:
			kind = Monthly
		case len(monthDays) > 0 && len(weekdays) > 0:
			return "", nil, 0, 0, errors.New("give either weekdays, month_days or nth_weekday, not several")
		case len(monthDays) > 0:
			kind = Monthly
		case len(weekdays) > 0:
			kind = Weekly
		default:
			return "", nil, 0, 0, errors.New("give a repeat kind or weekdays, month_days or nth_weekday")
		}
	}
	switch kind {
	case Once, Daily, Weekly, Monthly, Yearly:
	default:
		return "", nil, 0, 0, fmt.Errorf("repeat %q is not once, daily, weekly, monthly or yearly", repeat)
	}

	if kind != Weekly && len(weekdays) > 0 {
		return "", nil, 0, 0, fmt.Errorf("weekdays are only for repeat=weekly, got %s", kind)
	}
	if kind != Monthly && (len(monthDays) > 0 || nth != nil) {
		return "", nil, 0, 0, fmt.Errorf("month_days and nth_weekday are only for repeat=monthly, got %s", kind)
	}

	switch kind {
	case Weekly:
		days, err := parseWeekdays(weekdays)
		if err != nil {
			return "", nil, 0, 0, err
		}
		return kind, days, 0, 0, nil
	case Monthly:
		if nth != nil && len(monthDays) > 0 {
			return "", nil, 0, 0, errors.New("give either month_days or nth_weekday, not both")
		}
		if nth != nil {
			position := nth.Position
			if position < -1 || position == 0 || position > 4 {
				return "", nil, 0, 0, fmt.Errorf("nth_weekday.position must be 1..4 or -1, got %d", position)
			}
			days, err := parseWeekdays([]string{nth.Weekday})
			if err != nil || len(days) != 1 {
				return "", nil, 0, 0, fmt.Errorf("nth_weekday.weekday %q is not recognised", nth.Weekday)
			}
			return kind, nil, position, days[0], nil
		}
		days := append([]int(nil), monthDays...)
		for _, day := range days {
			if day < 1 || day > 31 {
				return "", nil, 0, 0, fmt.Errorf("month_days must be between 1 and 31, got %d", day)
			}
		}
		return kind, days, 0, 0, nil
	default:
		return kind, nil, 0, 0, nil
	}
}

// parseMCPEnd maps until/count to the service end condition. Neither means "keep
// the current end" for an update and "never" for a create.
func parseMCPEnd(until string, count int) (EndKind, string, int, error) {
	until = strings.TrimSpace(until)
	if until != "" && count > 0 {
		return "", "", 0, errors.New("give either until or count, not both")
	}
	switch {
	case until != "":
		return EndUntil, until, 0, nil
	case count > 0:
		return EndCount, "", count, nil
	default:
		return "", "", 0, nil
	}
}

// inputFromReminder rebuilds the full service input of a stored reminder, which
// is the base of a partial update.
func inputFromReminder(reminder Reminder) Input {
	input := Input{
		Message:           reminder.Message,
		Timezone:          reminder.Timezone,
		Kind:              reminder.Kind,
		Days:              append([]int(nil), reminder.Days...),
		Interval:          reminder.Interval,
		MonthWeekPosition: reminder.MonthWeekPosition,
		MonthWeekday:      reminder.MonthWeekday,
		EndKind:           reminder.End.Kind,
		EndCount:          reminder.End.Count,
		TimeHHMM:          reminder.TimeHHMM,
		RepeatMinutes:     reminder.RepeatMinutes,
		Enabled:           reminder.Enabled,
	}
	if !reminder.StartDate.IsZero() {
		input.StartDate = reminder.StartDate.String()
	}
	if !reminder.End.Date.IsZero() {
		input.EndDate = reminder.End.Date.String()
	}
	return input
}

// valueOrZero unwraps an optional count.
func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// parseWeekdays accepts the short day names and ISO numbers.
func parseWeekdays(values []string) ([]int, error) {
	days := make([]int, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if index := slices.Index(weekdayNames, value); index >= 0 {
			days = append(days, index+1)
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil || number < 1 || number > 7 {
			return nil, fmt.Errorf(
				"weekday %q is not recognised: use mon,tue,wed,thu,fri,sat,sun or 1..7", value,
			)
		}
		days = append(days, number)
	}
	return days, nil
}

// mcpServiceError maps the service errors to messages an LLM can act on. The
// service validation text is already user-facing (Russian) and is passed
// through unchanged.
func mcpServiceError(err error) error {
	var invalid *ValidationError
	switch {
	case errors.As(err, &invalid):
		return errors.New(invalid.Message)
	case errors.Is(err, ErrNotFound):
		return errors.New("reminder not found: it does not exist or belongs to another user")
	case errors.Is(err, ErrNoActiveOccurrence):
		return errors.New("nothing to confirm: the reminder has not fired yet, was already completed, or was edited")
	default:
		return errors.New("reminder operation failed: " + err.Error())
	}
}

// reminderToMCP maps a service reminder to the tool answer.
func reminderToMCP(reminder Reminder) mcpReminder {
	out := mcpReminder{
		ID:               reminder.ID,
		Message:          reminder.Message,
		Repeat:           string(reminder.Kind),
		ScheduleKind:     string(reminder.Kind),
		Interval:         reminder.Interval,
		StartDate:        reminder.StartDate.String(),
		OccurrencesFired: reminder.OccurrencesFired,
		Finished:         reminder.Finished(),
		Time:             reminder.TimeHHMM,
		Timezone:         reminder.Timezone,
		RepeatMinutes:    reminder.RepeatMinutes,
		Enabled:          reminder.Enabled,
		CreatedAt:        formatMCPTime(reminder.CreatedAt),
		UpdatedAt:        formatMCPTime(reminder.UpdatedAt),
	}
	switch {
	case reminder.Kind == Weekly:
		out.Weekdays = weekdayList(reminder.Days)
	case reminder.Kind == Monthly && reminder.MonthWeekPosition != 0:
		out.NthWeekday = &mcpNthWeekday{
			Position: reminder.MonthWeekPosition,
			Weekday:  weekdayList([]int{reminder.MonthWeekday})[0],
		}
	case reminder.Kind == Monthly:
		out.MonthDays = append([]int(nil), reminder.Days...)
	}
	if reminder.End.Kind == EndUntil {
		out.Until = reminder.End.Date.String()
	}
	if reminder.End.Kind == EndCount {
		out.Count = reminder.End.Count
	}
	if reminder.NextDueAt != nil {
		out.NextDueAt = formatMCPTime(*reminder.NextDueAt)
	}
	if reminder.LastError != nil {
		out.LastError = *reminder.LastError
	}
	if reminder.FinishedAt != nil {
		out.FinishedAt = formatMCPTime(*reminder.FinishedAt)
	}
	if reminder.ActiveOccurrence != nil {
		occurrence := occurrenceToMCP(*reminder.ActiveOccurrence)
		out.ActiveOccurrence = &occurrence
	}
	return out
}

func occurrenceToMCP(occurrence Occurrence) mcpOccurrence {
	out := mcpOccurrence{
		ID:          occurrence.ID,
		Status:      string(occurrence.Status),
		ScheduledAt: formatMCPTime(occurrence.ScheduledAt),
		RepeatCount: occurrence.RepeatCount,
	}
	if occurrence.FirstSentAt != nil {
		out.FirstSentAt = formatMCPTime(*occurrence.FirstSentAt)
	}
	if occurrence.NextRepeatAt != nil {
		out.NextRepeatAt = formatMCPTime(*occurrence.NextRepeatAt)
	}
	if occurrence.CompletedAt != nil {
		out.CompletedAt = formatMCPTime(*occurrence.CompletedAt)
	}
	return out
}

// weekdayList renders ISO weekdays as names. An unknown number is kept as a
// string so a corrupt row cannot crash a read tool.
func weekdayList(days []int) []string {
	out := make([]string, 0, len(days))
	for _, day := range days {
		if day >= 1 && day <= len(weekdayNames) {
			out = append(out, weekdayNames[day-1])
			continue
		}
		out = append(out, strconv.Itoa(day))
	}
	return out
}

// formatMCPTime renders an instant as RFC 3339 in UTC.
func formatMCPTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
