package reminders

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// ErrSpaceNotFound means the named reminders space does not exist or the user is
// not a member of it.
var ErrSpaceNotFound = errors.New("reminders: space not found")

// resolveSpace turns the space named by a client into its id, checking that the
// user is a member of it and that it is a reminders space. An empty name means
// the user's default space, created on demand.
func resolveSpace(ctx context.Context, q *db.Queries, userID pgtype.UUID, spaceID string) (pgtype.UUID, error) {
	if spaceID == "" {
		row, err := workspace.DefaultTx(ctx, q, userID, workspace.AppReminders)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("default reminders space: %w", err)
		}
		return row.ID, nil
	}
	id, err := workspace.ParseID(spaceID)
	if err != nil {
		return pgtype.UUID{}, ErrSpaceNotFound
	}
	row, err := q.GetWorkspaceForUser(ctx, db.GetWorkspaceForUserParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.App != workspace.AppReminders) {
		return pgtype.UUID{}, ErrSpaceNotFound
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("get reminders space: %w", err)
	}
	return row.ID, nil
}

// setRecipients replaces the recipients of a reminder. Every one of them must be
// a member of the reminder's space, and there must be at least one.
func setRecipients(ctx context.Context, q *db.Queries, reminder db.Reminder, recipients []string) ([]string, error) {
	if len(recipients) == 0 {
		return nil, validationErrorf("Выберите хотя бы одного получателя.")
	}
	if err := q.ClearReminderRecipients(ctx, reminder.ID); err != nil {
		return nil, fmt.Errorf("clear recipients: %w", err)
	}
	for _, recipient := range recipients {
		id, err := workspace.ParseID(recipient)
		if err != nil {
			return nil, validationErrorf("Неизвестный получатель.")
		}
		member, err := q.IsWorkspaceMember(ctx, db.IsWorkspaceMemberParams{WorkspaceID: reminder.WorkspaceID, UserID: id})
		if err != nil {
			return nil, fmt.Errorf("check recipient: %w", err)
		}
		if !member {
			return nil, validationErrorf("Получатель должен быть участником пространства.")
		}
		if err := q.AddReminderRecipient(ctx, db.AddReminderRecipientParams{ReminderID: reminder.ID, UserID: id}); err != nil {
			return nil, fmt.Errorf("add recipient: %w", err)
		}
	}
	stored, err := recipientsOf(ctx, q, reminder.ID)
	if err != nil {
		return nil, err
	}
	return stored[formatUUID(reminder.ID)], nil
}

// recipientsOf returns the current recipients of reminders by reminder id.
func recipientsOf(ctx context.Context, q *db.Queries, reminderIDs ...pgtype.UUID) (map[string][]string, error) {
	rows, err := q.ListReminderRecipients(ctx, reminderIDs)
	if err != nil {
		return nil, fmt.Errorf("list recipients: %w", err)
	}
	out := make(map[string][]string, len(reminderIDs))
	for _, row := range rows {
		id := formatUUID(row.ReminderID)
		out[id] = append(out[id], formatUUID(row.UserID))
	}
	return out, nil
}

// deliveryTargets are the users a firing goes to: the chosen recipients, or,
// when all of them have left the space, every current member, so a shared
// reminder never goes silent.
func deliveryTargets(ctx context.Context, q *db.Queries, reminder db.Reminder) ([]string, error) {
	chosen, err := recipientsOf(ctx, q, reminder.ID)
	if err != nil {
		return nil, err
	}
	if targets := chosen[formatUUID(reminder.ID)]; len(targets) > 0 {
		return targets, nil
	}
	members, err := q.ListWorkspaceMembers(ctx, reminder.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	targets := make([]string, 0, len(members))
	for _, member := range members {
		targets = append(targets, formatUUID(member.ID))
	}
	return targets, nil
}

// ResolveSpace finds a reminders space the user named by id or name.
func (s *Service) ResolveSpace(ctx context.Context, principalID, ref string) (string, error) {
	space, err := workspace.New(s.st).Resolve(ctx, principalID, workspace.AppReminders, ref)
	var invalid *workspace.ValidationError
	if errors.As(err, &invalid) {
		return "", validationErrorf("%s", invalid.Message)
	}
	if err != nil {
		return "", err
	}
	return space.ID, nil
}
