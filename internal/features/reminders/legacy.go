package reminders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// legacyPlatform is the messenger of the old bot. Only a Telegram binding can
// claim an imported row, because the old table is keyed by Telegram user id.
const legacyPlatform = "telegram"

// LegacyImportOptions tunes an import run.
type LegacyImportOptions struct {
	// DryRun parses, validates and reports without writing anything.
	DryRun bool
}

// LegacyImportResult is what one import run did.
type LegacyImportResult struct {
	// Format is the export format that was parsed.
	Format LegacyFormat
	// Total is how many data rows the export contains.
	Total int
	// Imported is how many waiting rows were stored now.
	Imported int
	// Duplicates is how many rows of the export were imported earlier; they are
	// skipped, which is what makes a repeated import harmless.
	Duplicates int
	// Deleted is how many rows are marked deleted in the old table.
	Deleted int
	// Invalid lists the rows that failed validation, with the reason.
	Invalid []LegacyIssue
	// Attached is how many reminders were created immediately for users who had
	// already bound their Telegram account.
	Attached int
	// Pending is how many waiting rows the whole table holds after the run.
	Pending int
}

// ImportLegacy stores the valid rows of an export in the waiting room and
// attaches the ones whose Telegram account is already bound.
//
// Only configuration is imported: a reminder is created through the service, so
// it starts fresh with next_due_at computed from the current time, without any
// of the in-flight state (pending confirmations, message ids, locks) of the old
// workflow.
func (s *Service) ImportLegacy(ctx context.Context, export LegacyExport, opts LegacyImportOptions) (LegacyImportResult, error) {
	result := LegacyImportResult{
		Format:  export.Format,
		Total:   export.Total,
		Deleted: export.Deleted,
		Invalid: export.Invalid,
	}

	if opts.DryRun {
		existing, err := s.existingLegacyIDs(ctx, export.Rows)
		if err != nil {
			return result, err
		}
		result.Duplicates = len(existing)
		if result.Pending, err = s.pendingLegacyCount(ctx); err != nil {
			return result, err
		}
		return result, nil
	}

	telegramUserIDs := make([]string, 0, len(export.Rows))
	seen := make(map[string]struct{}, len(export.Rows))
	for _, row := range export.Rows {
		if _, err := s.st.Queries().InsertLegacyReminder(ctx, db.InsertLegacyReminderParams{
			SourceReminderID: row.SourceReminderID,
			TelegramUserID:   row.TelegramUserID,
			Message:          row.Input.Message,
			Timezone:         row.Input.Timezone,
			ScheduleKind:     string(row.Input.Kind),
			ScheduleDays:     daysToInt16(row.Input.Days),
			TimeHhmm:         row.Input.TimeHHMM,
			RepeatMinutes:    int32(row.Input.RepeatMinutes),
			Enabled:          row.Input.Enabled,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// ON CONFLICT DO NOTHING: the source row was imported before.
				result.Duplicates++
			} else {
				return result, fmt.Errorf("store legacy reminder %s: %w", row.SourceReminderID, err)
			}
		} else {
			result.Imported++
		}
		if _, ok := seen[row.TelegramUserID]; !ok {
			seen[row.TelegramUserID] = struct{}{}
			telegramUserIDs = append(telegramUserIDs, row.TelegramUserID)
		}
	}

	// Users who signed in before the import get their reminders right away,
	// through the same claim the binding path uses. A failure only leaves the
	// rows waiting: the import itself already stored them.
	sort.Strings(telegramUserIDs)
	for _, telegramUserID := range telegramUserIDs {
		userID, found, err := s.legacyBindingUser(ctx, telegramUserID)
		switch {
		case err != nil:
			return result, err
		case !found:
			continue
		}
		created, err := s.ClaimLegacyReminders(ctx, userID, telegramUserID)
		if err != nil {
			s.logger.ErrorContext(ctx, "attach imported reminders failed",
				slog.String("telegram_user_id", telegramUserID),
				slog.String("error", err.Error()),
			)
			continue
		}
		result.Attached += created
	}

	pending, err := s.pendingLegacyCount(ctx)
	if err != nil {
		return result, err
	}
	result.Pending = pending
	return result, nil
}

// ClaimLegacyReminders turns the waiting rows of a Telegram account into
// reminders of the user and marks them claimed.
//
// The whole claim is one transaction: either every pending row of the account
// becomes a reminder, or none of them does and they stay waiting. Two claims of
// the same account (two confirmations racing, or a confirm and an import) cannot
// duplicate a reminder: the claim takes rows with FOR UPDATE SKIP LOCKED and
// only while claimed_by IS NULL, and the reminders are created from the very
// rows it took.
func (s *Service) ClaimLegacyReminders(ctx context.Context, principalID, telegramUserID string) (int, error) {
	userID, err := parsePrincipal(principalID)
	if err != nil {
		return 0, err
	}
	telegramUserID = strings.TrimSpace(telegramUserID)
	if telegramUserID == "" {
		return 0, nil
	}

	created := 0
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		rows, err := q.ClaimLegacyReminders(ctx, db.ClaimLegacyRemindersParams{
			ClaimedBy:      userID,
			TelegramUserID: telegramUserID,
		})
		if err != nil {
			return fmt.Errorf("claim legacy reminders of %s: %w", telegramUserID, err)
		}
		for _, row := range rows {
			if _, err := s.CreateTx(ctx, q, principalID, legacyInput(row)); err != nil {
				var validation *ValidationError
				if errors.As(err, &validation) {
					// A stored row that no longer validates must not block the
					// rest of the account. It stays in the table as claimed and
					// the reason is logged.
					s.logger.WarnContext(ctx, "imported reminder was rejected",
						slog.String("source_reminder_id", row.SourceReminderID),
						slog.String("error", err.Error()),
					)
					continue
				}
				return fmt.Errorf("create reminder of %s: %w", row.SourceReminderID, err)
			}
			created++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return created, nil
}

// legacyBindingUser finds the principal behind a bound Telegram account. A
// messenger account without a binding is reported as not found.
func (s *Service) legacyBindingUser(ctx context.Context, telegramUserID string) (string, bool, error) {
	binding, err := s.st.Queries().GetBinding(ctx, db.GetBindingParams{
		Platform:     legacyPlatform,
		RemoteUserID: telegramUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve telegram binding of %s: %w", telegramUserID, err)
	}
	return formatUUID(binding.UserID), true, nil
}

// existingLegacyIDs reports which rows of an export are already stored.
func (s *Service) existingLegacyIDs(ctx context.Context, rows []LegacyRow) ([]string, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SourceReminderID)
	}
	existing, err := s.st.Queries().ExistingLegacySourceIDs(ctx, ids)
	if isUndefinedTable(err) {
		// A dry run never creates the table, so a database that has not seen an
		// import yet simply has no duplicates.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("look up imported legacy reminders: %w", err)
	}
	return existing, nil
}

// pendingLegacyCount is how many rows still wait for a binding.
func (s *Service) pendingLegacyCount(ctx context.Context) (int, error) {
	pending, err := s.st.Queries().CountPendingLegacyReminders(ctx)
	if isUndefinedTable(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("count waiting legacy reminders: %w", err)
	}
	return int(pending), nil
}

// isUndefinedTable reports PostgreSQL's "relation does not exist". It lets a dry
// run work against a database where the import table was never created.
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// legacyInput rebuilds the rule of a stored waiting row.
func legacyInput(row db.LegacyRemindersImport) Input {
	return Input{
		Message:       row.Message,
		Timezone:      row.Timezone,
		Kind:          schedule.Kind(row.ScheduleKind),
		Days:          daysToInt(row.ScheduleDays),
		TimeHHMM:      row.TimeHhmm,
		RepeatMinutes: int(row.RepeatMinutes),
		Enabled:       row.Enabled,
	}
}
