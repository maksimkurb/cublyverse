package httpapi

import (
	"context"
	"errors"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// User-facing messages of the reminders endpoints.
const (
	msgReminderNotFound   = "Напоминание не найдено"
	msgNoActiveOccurrence = "У напоминания нет активного срабатывания"
)

// ListReminders implements GET /api/v1/reminders.
func (h *handlers) ListReminders(ctx context.Context, request gen.ListRemindersRequestObject) (gen.ListRemindersResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListReminders401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	space := ""
	if request.Params.SpaceId != nil {
		space = *request.Params.SpaceId
	}
	items, err := h.deps.Reminders.ListSpace(ctx, principal, space)
	if errors.Is(err, reminders.ErrSpaceNotFound) {
		return gen.ListReminders200JSONResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(gen.ListReminders200JSONResponse, 0, len(items))
	for _, item := range items {
		out = append(out, reminderToAPI(item))
	}
	return out, nil
}

// CreateReminder implements POST /api/v1/reminders.
func (h *handlers) CreateReminder(ctx context.Context, request gen.CreateReminderRequestObject) (gen.CreateReminderResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateReminder401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.CreateReminder400JSONResponse(errorPayload(codeBadRequest, "Некорректный запрос")), nil
	}
	reminder, err := h.deps.Reminders.Create(ctx, principal, createReminderInput(*request.Body))
	if err != nil {
		if errors.Is(err, reminders.ErrSpaceNotFound) {
			return gen.CreateReminder400JSONResponse(errorPayload(codeBadRequest, msgWorkspaceNotFound)), nil
		}
		var invalid *reminders.ValidationError
		if errors.As(err, &invalid) {
			return gen.CreateReminder400JSONResponse(errorPayload(codeBadRequest, invalid.Message)), nil
		}
		return nil, err
	}
	return gen.CreateReminder201JSONResponse(reminderToAPI(reminder)), nil
}

// GetReminder implements GET /api/v1/reminders/{id}.
func (h *handlers) GetReminder(ctx context.Context, request gen.GetReminderRequestObject) (gen.GetReminderResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.GetReminder401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	reminder, err := h.deps.Reminders.Get(ctx, principal, request.Id)
	if err != nil {
		if errors.Is(err, reminders.ErrNotFound) {
			return gen.GetReminder404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgReminderNotFound)),
			}, nil
		}
		return nil, err
	}
	return gen.GetReminder200JSONResponse(reminderToAPI(reminder)), nil
}

// UpdateReminder implements PUT /api/v1/reminders/{id}.
func (h *handlers) UpdateReminder(ctx context.Context, request gen.UpdateReminderRequestObject) (gen.UpdateReminderResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateReminder401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UpdateReminder400JSONResponse(errorPayload(codeBadRequest, "Некорректный запрос")), nil
	}
	reminder, err := h.deps.Reminders.Update(ctx, principal, request.Id, updateReminderInput(*request.Body))
	if err != nil {
		var invalid *reminders.ValidationError
		switch {
		case errors.Is(err, reminders.ErrSpaceNotFound):
			return gen.UpdateReminder400JSONResponse(errorPayload(codeBadRequest, msgWorkspaceNotFound)), nil
		case errors.As(err, &invalid):
			return gen.UpdateReminder400JSONResponse(errorPayload(codeBadRequest, invalid.Message)), nil
		case errors.Is(err, reminders.ErrNotFound):
			return gen.UpdateReminder404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgReminderNotFound)),
			}, nil
		}
		return nil, err
	}
	return gen.UpdateReminder200JSONResponse(reminderToAPI(reminder)), nil
}

// DeleteReminder implements DELETE /api/v1/reminders/{id}.
func (h *handlers) DeleteReminder(ctx context.Context, request gen.DeleteReminderRequestObject) (gen.DeleteReminderResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteReminder401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Reminders.Delete(ctx, principal, request.Id); err != nil {
		if errors.Is(err, reminders.ErrNotFound) {
			return gen.DeleteReminder404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgReminderNotFound)),
			}, nil
		}
		return nil, err
	}
	return gen.DeleteReminder204Response{}, nil
}

// CompleteReminder implements POST /api/v1/reminders/{id}/complete.
func (h *handlers) CompleteReminder(ctx context.Context, request gen.CompleteReminderRequestObject) (gen.CompleteReminderResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CompleteReminder401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	reminder, err := h.deps.Reminders.Complete(ctx, principal, request.Id)
	if err != nil {
		switch {
		case errors.Is(err, reminders.ErrNotFound):
			return gen.CompleteReminder404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgReminderNotFound)),
			}, nil
		case errors.Is(err, reminders.ErrNoActiveOccurrence):
			return gen.CompleteReminder409JSONResponse(
				errorPayload(codeConflict, msgNoActiveOccurrence),
			), nil
		}
		return nil, err
	}
	return gen.CompleteReminder200JSONResponse(reminderToAPI(reminder)), nil
}

// principalID returns the principal of the request session.
func (h *handlers) principalID(ctx context.Context) (string, bool) {
	session, ok := sessionFrom(ctx)
	if !ok || session.PrincipalID == "" {
		return "", false
	}
	return session.PrincipalID, true
}

// createReminderInput maps the request body to the service input, applying the
// documented defaults for the optional fields.
func createReminderInput(body gen.CreateReminderRequest) reminders.Input {
	input := reminders.Input{
		Message:       body.Message,
		Timezone:      body.Timezone,
		Kind:          reminders.Kind(body.ScheduleKind),
		Days:          daysFromAPI(body.ScheduleDays),
		TimeHHMM:      body.Time,
		RepeatMinutes: reminders.DefaultRepeatMinutes,
		Enabled:       true,
		SpaceID:       stringValue(body.SpaceId),
	}
	if body.RecipientIds != nil {
		input.Recipients = *body.RecipientIds
	}
	applyRecurrence(&input, body.Interval, body.MonthWeekPosition, body.MonthWeekday,
		body.StartDate, endKindName(body.EndKind), body.EndDate, body.EndCount)
	if body.RepeatMinutes != nil {
		input.RepeatMinutes = *body.RepeatMinutes
	}
	if body.Enabled != nil {
		input.Enabled = *body.Enabled
	}
	return input
}

// updateReminderInput maps the full replacement body to the service input.
func updateReminderInput(body gen.UpdateReminderRequest) reminders.Input {
	input := reminders.Input{
		Message:       body.Message,
		Timezone:      body.Timezone,
		Kind:          reminders.Kind(body.ScheduleKind),
		Days:          daysFromAPI(body.ScheduleDays),
		TimeHHMM:      body.Time,
		RepeatMinutes: body.RepeatMinutes,
		Enabled:       body.Enabled,
		SpaceID:       stringValue(body.SpaceId),
	}
	if body.RecipientIds != nil {
		input.Recipients = *body.RecipientIds
	}
	applyRecurrence(&input, body.Interval, body.MonthWeekPosition, body.MonthWeekday,
		body.StartDate, stringValue(body.EndKind), body.EndDate, body.EndCount)
	return input
}

// endKindName renders the optional create end kind as a string.
func endKindName(value *gen.CreateReminderRequestEndKind) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

// stringValue renders an optional generated enum as a string.
func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

// applyRecurrence copies the optional recurrence fields, leaving service
// defaults for everything a client omitted.
func applyRecurrence(
	input *reminders.Input,
	interval *int,
	monthWeekPosition *int,
	monthWeekday *int,
	startDate *openapi_types.Date,
	endKind string,
	endDate *openapi_types.Date,
	endCount *int,
) {
	if interval != nil {
		input.Interval = *interval
	}
	if monthWeekPosition != nil {
		input.MonthWeekPosition = *monthWeekPosition
	}
	if monthWeekday != nil {
		input.MonthWeekday = *monthWeekday
	}
	if startDate != nil {
		input.StartDate = startDate.String()
	}
	if endKind != "" {
		input.EndKind = reminders.EndKind(endKind)
	}
	if endDate != nil {
		input.EndDate = endDate.String()
	}
	if endCount != nil {
		input.EndCount = *endCount
	}
}

// daysFromAPI unwraps the optional day list. An omitted list is empty, which is
// exactly what a daily, yearly or one-time rule needs.
func daysFromAPI(days *[]int) []int {
	if days == nil {
		return nil
	}
	return *days
}

// reminderToAPI maps the service model to the API schema.
func reminderToAPI(reminder reminders.Reminder) gen.Reminder {
	recipients := reminder.Recipients
	if recipients == nil {
		recipients = []string{}
	}
	out := gen.Reminder{
		SpaceId:           reminder.SpaceID,
		AuthorId:          reminder.AuthorID,
		RecipientIds:      recipients,
		Id:                reminder.ID,
		Message:           reminder.Message,
		Timezone:          reminder.Timezone,
		ScheduleKind:      gen.ScheduleKind(reminder.Kind),
		ScheduleDays:      reminder.Days,
		Interval:          reminder.Interval,
		MonthWeekPosition: optionalInt(reminder.MonthWeekPosition),
		MonthWeekday:      optionalInt(reminder.MonthWeekday),
		StartDate:         dateToAPI(reminder.StartDate),
		EndKind:           gen.ReminderEndKind(reminder.End.Kind),
		EndDate:           optionalDateToAPI(reminder.End.Date),
		EndCount:          optionalInt(reminder.End.Count),
		OccurrencesFired:  reminder.OccurrencesFired,
		Finished:          reminder.Finished(),
		FinishedAt:        reminder.FinishedAt,
		Time:              reminder.TimeHHMM,
		RepeatMinutes:     reminder.RepeatMinutes,
		Enabled:           reminder.Enabled,
		NextDueAt:         reminder.NextDueAt,
		LastError:         reminder.LastError,
		CreatedAt:         reminder.CreatedAt,
		UpdatedAt:         reminder.UpdatedAt,
	}
	if reminder.ActiveOccurrence != nil {
		occurrence := occurrenceToAPI(*reminder.ActiveOccurrence)
		out.ActiveOccurrence = &occurrence
	}
	return out
}

// optionalInt maps the zero "not set" value to a JSON null.
func optionalInt(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

// dateToAPI converts a civil date to the API date type.
func dateToAPI(value reminders.Date) openapi_types.Date {
	return openapi_types.Date{Time: time.Date(value.Year, value.Month, value.Day, 0, 0, 0, 0, time.UTC)}
}

// optionalDateToAPI maps a zero date to a JSON null.
func optionalDateToAPI(value reminders.Date) *openapi_types.Date {
	if value.IsZero() {
		return nil
	}
	converted := dateToAPI(value)
	return &converted
}

// occurrenceToAPI maps the occurrence of the service model to the API schema.
func occurrenceToAPI(occurrence reminders.Occurrence) gen.ReminderOccurrence {
	return gen.ReminderOccurrence{
		Id:           occurrence.ID,
		ReminderId:   occurrence.ReminderID,
		ScheduledAt:  occurrence.ScheduledAt,
		FirstSentAt:  occurrence.FirstSentAt,
		RepeatCount:  occurrence.RepeatCount,
		NextRepeatAt: occurrence.NextRepeatAt,
		Status:       gen.ReminderOccurrenceStatus(occurrence.Status),
		CompletedAt:  occurrence.CompletedAt,
	}
}
