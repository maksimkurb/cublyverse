package reminders_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// legacyCSV loads the CSV fixture of the old n8n table.
func legacyCSV(t *testing.T) reminders.LegacyExport {
	t.Helper()
	data, err := os.ReadFile("testdata/legacy_reminders.csv")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	export, err := reminders.ParseLegacyExport(data, reminders.LegacyFormatAuto)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return export
}

// bindTelegram stores a verified Telegram binding, as the confirmation page
// does.
func (e *testEnv) bindTelegram(principalID, telegramUserID string) {
	e.t.Helper()
	if _, err := e.store.Queries().UpsertBinding(context.Background(), db.UpsertBindingParams{
		UserID:         parseTestUUID(e.t, principalID),
		Platform:       "telegram",
		RemoteUserID:   telegramUserID,
		ConversationID: telegramUserID,
		VerifiedAt:     fixedNow,
		Metadata:       []byte("{}"),
	}); err != nil {
		e.t.Fatalf("bind telegram %s → %s: %v", telegramUserID, principalID, err)
	}
}

// countRows counts the rows of a table. Tests assert the stored state directly,
// never through the code under test.
func (e *testEnv) countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := e.store.Pool().QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

// TestLegacyImportIdempotent covers a plain import and a repeated one: the
// second run reports the rows as duplicates and changes nothing.
func TestLegacyImportIdempotent(t *testing.T) {
	env := newTestEnv(t)
	export := legacyCSV(t)

	first, err := env.service.ImportLegacy(context.Background(), export, reminders.LegacyImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if first.Imported != 4 || first.Duplicates != 0 || first.Attached != 0 || first.Pending != 4 {
		t.Fatalf("first import = %+v, want 4 imported, nothing attached, 4 waiting", first)
	}
	if first.Deleted != 1 || len(first.Invalid) != 2 || first.Total != 7 {
		t.Fatalf("first import = %+v, want the deleted and invalid rows counted", first)
	}

	// Nobody is bound yet, so nothing became a reminder.
	if got := env.countRows(t, "SELECT count(*) FROM reminders"); got != 0 {
		t.Fatalf("reminders = %d, want 0", got)
	}

	second, err := env.service.ImportLegacy(context.Background(), export, reminders.LegacyImportOptions{})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Imported != 0 || second.Duplicates != 4 {
		t.Fatalf("second import = %+v, want 4 duplicates and no new rows", second)
	}
	if got := env.countRows(t, "SELECT count(*) FROM legacy_reminders_import"); got != 4 {
		t.Fatalf("waiting rows = %d, want 4", got)
	}
}

// TestLegacyImportDryRun proves --dry-run writes nothing, even when it reports
// the duplicates of an earlier import.
func TestLegacyImportDryRun(t *testing.T) {
	env := newTestEnv(t)
	export := legacyCSV(t)

	dry, err := env.service.ImportLegacy(context.Background(), export, reminders.LegacyImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.Imported != 0 || dry.Duplicates != 0 || dry.Pending != 0 {
		t.Fatalf("dry run = %+v, want nothing stored", dry)
	}
	if dry.Total != 7 || dry.Deleted != 1 || len(dry.Invalid) != 2 {
		t.Fatalf("dry run = %+v, want the full report", dry)
	}
	if got := env.countRows(t, "SELECT count(*) FROM legacy_reminders_import"); got != 0 {
		t.Fatalf("waiting rows after a dry run = %d, want 0", got)
	}

	// After a real import the dry run reports the duplicates without writing.
	if _, err := env.service.ImportLegacy(context.Background(), export, reminders.LegacyImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}
	dry, err = env.service.ImportLegacy(context.Background(), export, reminders.LegacyImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.Duplicates != 4 || dry.Pending != 4 || dry.Imported != 0 {
		t.Fatalf("dry run after import = %+v, want 4 duplicates and 4 waiting", dry)
	}
}

// TestLegacyImportAttachesAlreadyBoundUser covers the second import path: a user
// who signed in before the import gets the reminders immediately.
func TestLegacyImportAttachesAlreadyBoundUser(t *testing.T) {
	env := newTestEnv(t)
	alice := env.createUser("alice")
	env.bindTelegram(alice, "1001")

	result, err := env.service.ImportLegacy(context.Background(), legacyCSV(t), reminders.LegacyImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 4 || result.Attached != 2 || result.Pending != 2 {
		t.Fatalf("import = %+v, want 2 attached and 2 waiting", result)
	}

	list, err := env.service.List(context.Background(), alice)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("alice reminders = %d, want 2", len(list))
	}
	messages := map[string]reminders.Reminder{}
	for _, reminder := range list {
		messages[reminder.Message] = reminder
	}
	if _, ok := messages["Позвонить маме 👩‍👧"]; !ok {
		t.Errorf("weekly reminder is missing: %+v", list)
	}
	if _, ok := messages["Оплатить коммуналку 💸\nи проверить счёт"]; !ok {
		t.Errorf("monthly reminder is missing: %+v", list)
	}
	// The imported rule starts fresh: next_due_at is computed from now, not the
	// 2020 timestamp of the export, and no occurrence is inherited.
	for _, reminder := range list {
		if reminder.NextDueAt == nil || !reminder.NextDueAt.After(fixedNow) {
			t.Errorf("next_due_at = %v, want a future time", reminder.NextDueAt)
		}
		if reminder.ActiveOccurrence != nil {
			t.Errorf("imported reminder has an active occurrence: %+v", reminder.ActiveOccurrence)
		}
	}
	if got := env.countRows(t, "SELECT count(*) FROM reminder_occurrences"); got != 0 {
		t.Fatalf("occurrences = %d, want 0", got)
	}
	if got := env.countRows(t, "SELECT count(*) FROM legacy_reminders_import WHERE claimed_by = $1",
		parseTestUUID(t, alice)); got != 2 {
		t.Fatalf("claimed rows = %d, want 2", got)
	}
}

// TestLegacyClaimOnBind covers the binding path: the waiting rows of a freshly
// bound Telegram account become reminders of that user, exactly once.
func TestLegacyClaimOnBind(t *testing.T) {
	env := newTestEnv(t)
	bob := env.createUser("bob")

	if _, err := env.service.ImportLegacy(context.Background(), legacyCSV(t), reminders.LegacyImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := env.countRows(t, "SELECT count(*) FROM reminders"); got != 0 {
		t.Fatalf("reminders before the binding = %d, want 0", got)
	}

	claimed, err := env.service.ClaimLegacyReminders(context.Background(), bob, "1001")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed != 2 {
		t.Fatalf("claimed = %d, want 2", claimed)
	}

	list, err := env.service.List(context.Background(), bob)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("bob reminders = %d, want 2", len(list))
	}

	// A second claim of the same account creates nothing: both confirmations and
	// a re-run of the import are harmless.
	again, err := env.service.ClaimLegacyReminders(context.Background(), bob, "1001")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if again != 0 {
		t.Fatalf("second claim = %d, want 0", again)
	}
	if got := env.countRows(t, "SELECT count(*) FROM reminders"); got != 2 {
		t.Fatalf("reminders = %d, want 2", got)
	}

	// The other account of the export is still waiting.
	if got := env.countRows(t, "SELECT count(*) FROM legacy_reminders_import WHERE claimed_by IS NULL"); got != 2 {
		t.Fatalf("waiting rows = %d, want 2", got)
	}

	// An account without waiting rows claims nothing.
	other, err := env.service.ClaimLegacyReminders(context.Background(), bob, "999999")
	if err != nil {
		t.Fatalf("claim of an unknown account: %v", err)
	}
	if other != 0 {
		t.Fatalf("unknown account claim = %d, want 0", other)
	}
}

// TestLegacyClaimSkipsUnusableRow proves one stored row that no longer passes
// the reminder rules cannot block the rest of the account: it is claimed (so it
// is not retried forever) and logged, the others still become reminders.
func TestLegacyClaimSkipsUnusableRow(t *testing.T) {
	env := newTestEnv(t)
	dave := env.createUser("dave")

	ctx := context.Background()
	if _, err := env.store.Pool().Exec(ctx, `
		INSERT INTO legacy_reminders_import (
			source_reminder_id, telegram_user_id, message, timezone,
			schedule_kind, schedule_days, time_hhmm, repeat_minutes, enabled
		) VALUES
			('broken-row', '1001', 'Марсианский пояс', 'Mars/Olympus',
			 'weekly', ARRAY[1]::smallint[], '10:00', 10, true),
			('good-row', '1001', 'Рабочее напоминание', 'Europe/Moscow',
			 'weekly', ARRAY[2]::smallint[], '11:00', 10, true)`,
	); err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	claimed, err := env.service.ClaimLegacyReminders(ctx, dave, "1001")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("claimed = %d, want 1 (the unusable row is skipped)", claimed)
	}
	list, err := env.service.List(ctx, dave)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Message != "Рабочее напоминание" {
		t.Fatalf("reminders = %+v, want only the usable one", list)
	}
	// The unusable row is marked as handled, not retried on every claim.
	if got := env.countRows(t, "SELECT count(*) FROM legacy_reminders_import WHERE claimed_by IS NULL"); got != 0 {
		t.Fatalf("waiting rows = %d, want 0", got)
	}
}

// TestLegacyClaimRace proves two claims of the same Telegram account cannot
// create the same reminder twice. Run with -race.
func TestLegacyClaimRace(t *testing.T) {
	env := newTestEnv(t)
	carol := env.createUser("carol")

	if _, err := env.service.ImportLegacy(context.Background(), legacyCSV(t), reminders.LegacyImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}

	const claimers = 4
	start := make(chan struct{})
	counts := make([]int, claimers)
	errs := make([]error, claimers)
	var wg sync.WaitGroup
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			counts[index], errs[index] = env.service.ClaimLegacyReminders(context.Background(), carol, "1001")
		}(i)
	}
	close(start)
	wg.Wait()

	total := 0
	for index := range counts {
		if errs[index] != nil {
			t.Fatalf("claimer %d: %v", index, errs[index])
		}
		total += counts[index]
	}
	if total != 2 {
		t.Fatalf("claimers created %d reminders in total, want 2 (counts %v)", total, counts)
	}

	ctx := context.Background()
	list, err := env.service.List(ctx, carol)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("carol reminders = %d, want 2", len(list))
	}
	// Every reminder of the account is created exactly once: two distinct rules.
	seen := map[string]bool{}
	for _, reminder := range list {
		if seen[reminder.Message] {
			t.Fatalf("duplicate imported reminder: %q", reminder.Message)
		}
		seen[reminder.Message] = true
		if reminder.NextDueAt == nil || !reminder.NextDueAt.After(fixedNow.Add(-time.Second)) {
			t.Errorf("next_due_at = %v, want a fresh future time", reminder.NextDueAt)
		}
	}
}
