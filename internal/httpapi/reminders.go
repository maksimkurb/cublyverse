package httpapi

import (
	"context"
	"errors"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// User-facing messages of the reminders endpoints.
const (
	msgReminderNotFound   = "Напоминание не найдено"
	msgNoActiveOccurrence = "У напоминания нет активного срабатывания"
)

// ListReminders implements GET /api/v1/reminders.
func (h *handlers) ListReminders(ctx context.Context, _ gen.ListRemindersRequestObject) (gen.ListRemindersResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListReminders401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	items, err := h.deps.Reminders.List(ctx, principal)
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
		Days:          body.ScheduleDays,
		TimeHHMM:      body.Time,
		RepeatMinutes: reminders.DefaultRepeatMinutes,
		Enabled:       true,
	}
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
	return reminders.Input{
		Message:       body.Message,
		Timezone:      body.Timezone,
		Kind:          reminders.Kind(body.ScheduleKind),
		Days:          body.ScheduleDays,
		TimeHHMM:      body.Time,
		RepeatMinutes: body.RepeatMinutes,
		Enabled:       body.Enabled,
	}
}

// reminderToAPI maps the service model to the API schema.
func reminderToAPI(reminder reminders.Reminder) gen.Reminder {
	out := gen.Reminder{
		Id:            reminder.ID,
		Message:       reminder.Message,
		Timezone:      reminder.Timezone,
		ScheduleKind:  gen.ScheduleKind(reminder.Kind),
		ScheduleDays:  reminder.Days,
		Time:          reminder.TimeHHMM,
		RepeatMinutes: reminder.RepeatMinutes,
		Enabled:       reminder.Enabled,
		NextDueAt:     reminder.NextDueAt,
		LastError:     reminder.LastError,
		CreatedAt:     reminder.CreatedAt,
		UpdatedAt:     reminder.UpdatedAt,
	}
	if reminder.ActiveOccurrence != nil {
		occurrence := occurrenceToAPI(*reminder.ActiveOccurrence)
		out.ActiveOccurrence = &occurrence
	}
	return out
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
