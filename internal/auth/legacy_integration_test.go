package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// legacyImport is a two-rule export of the old n8n bot for one Telegram account.
func legacyImport(telegramUserID string) reminders.LegacyExport {
	return reminders.LegacyExport{
		Format: reminders.LegacyFormatCSV,
		Total:  2,
		Rows: []reminders.LegacyRow{
			{
				SourceReminderID: "legacy-1",
				TelegramUserID:   telegramUserID,
				Input: reminders.Input{
					Message:       "Полить цветы 🌱",
					Timezone:      "Europe/Moscow",
					Kind:          reminders.Weekly,
					Days:          []int{1, 4},
					TimeHHMM:      "08:15",
					RepeatMinutes: 10,
					Enabled:       true,
				},
			},
			{
				SourceReminderID: "legacy-2",
				TelegramUserID:   telegramUserID,
				Input: reminders.Input{
					Message:       "Оплатить подписки",
					Timezone:      "Europe/Moscow",
					Kind:          reminders.Monthly,
					Days:          []int{1, 15},
					TimeHHMM:      "19:45",
					RepeatMinutes: 30,
					Enabled:       true,
				},
			},
		},
	}
}

// claimRecorder is the OnLegacyImported hook of the tests.
type claimRecorder struct {
	mu    sync.Mutex
	calls []int
}

func (r *claimRecorder) record(_ context.Context, _ unibot.Binding, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, count)
}

func (r *claimRecorder) counts() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.calls...)
}

// confirmTelegramBinding walks the whole Mini App linking flow of one Telegram
// account: initData verification, the SSO round trip, the confirmation page and
// the confirmation POST.
func confirmTelegramBinding(t *testing.T, app *testApp, idp *fakeIDP, telegramUserID int64) {
	t.Helper()
	client := app.newClient()

	resp, body := client.postJSON("/api/v1/auth/telegram",
		fmt.Sprintf(`{"init_data":%q}`, initData(t, telegramUserID, time.Now())))
	requireStatus(t, resp, http.StatusOK, body)
	linkURL := decodeJSON[telegramLoginResponse](t, body).LinkURL
	parsed, err := url.Parse(linkURL)
	if err != nil {
		t.Fatalf("parse link_url %q: %v", linkURL, err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("link_url has no state")
	}

	resp, body = client.do(http.MethodPost, app.baseURL+"/auth/telegram/verify",
		fmt.Sprintf(`{"state":%q,"init_data":%q}`, state, initData(t, telegramUserID, time.Now())),
		"application/json", "")
	requireStatus(t, resp, http.StatusOK, body)

	resp, body = client.fetch(decodeJSON[map[string]string](t, body)["redirect_url"])
	requireStatus(t, resp, http.StatusFound, body)
	resp, body = client.do(http.MethodGet, location(t, resp), "", "", "")
	requireStatus(t, resp, http.StatusFound, body)
	resp, body = client.do(http.MethodGet, location(t, resp), "", "", "")
	requireStatus(t, resp, http.StatusOK, body)

	resp, body = client.postForm("/auth/confirm", url.Values{
		"state": {state},
		"token": {formToken(t, body)},
	})
	requireStatus(t, resp, http.StatusOK, body)
}

// TestTelegramBindingClaimsImportedReminders covers the hand-over at binding
// time: the rows of the old n8n bot that wait for this Telegram account become
// reminders of the SSO user, and the user is told how many.
func TestTelegramBindingClaimsImportedReminders(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	idp.signIn("subject-legacy", "legacy@example.com", "Старый Пользователь")

	const telegramUserID = 279058397
	service := reminders.New(app.store, reminders.WithLogger(testLogger()))
	app.auth.Legacy = service
	recorder := &claimRecorder{}
	app.auth.OnLegacyImported = recorder.record

	if _, err := service.ImportLegacy(context.Background(), legacyImport("279058397"), reminders.LegacyImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}
	// Nothing is imported before the account exists: the rows wait.
	if got := app.legacyReminderCount(t, "SELECT count(*) FROM reminders"); got != 0 {
		t.Fatalf("reminders before the binding = %d, want 0", got)
	}

	confirmTelegramBinding(t, app, idp, telegramUserID)

	if got := app.bindingCount(t); got != 1 {
		t.Fatalf("bindings = %d, want 1", got)
	}
	principal, ok, err := app.auth.Resolve(context.Background(), unibot.PlatformTelegram, "279058397")
	if err != nil || !ok {
		t.Fatalf("resolving the binding = (%q, %v, %v)", principal, ok, err)
	}
	list, err := service.List(context.Background(), string(principal))
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("imported reminders = %d, want 2", len(list))
	}
	for _, reminder := range list {
		if reminder.Message != "Полить цветы 🌱" && reminder.Message != "Оплатить подписки" {
			t.Errorf("unexpected imported reminder: %+v", reminder)
		}
		if reminder.NextDueAt == nil {
			t.Errorf("imported reminder %q has no next_due_at", reminder.Message)
		}
	}
	if got := app.legacyReminderCount(t, "SELECT count(*) FROM legacy_reminders_import WHERE claimed_by IS NULL"); got != 0 {
		t.Fatalf("waiting rows = %d, want 0", got)
	}

	// The chat is told about the import exactly once, after the binding.
	if counts := recorder.counts(); len(counts) != 1 || counts[0] != 2 {
		t.Fatalf("OnLegacyImported calls = %v, want one call with 2", counts)
	}
}

// failingClaimer simulates a broken import: the claim fails, the binding must
// still be created.
type failingClaimer struct{}

func (failingClaimer) ClaimLegacyReminders(context.Context, string, string) (int, error) {
	return 0, errors.New("legacy import is broken")
}

// TestTelegramBindingSurvivesClaimFailure covers the decision that a claim is
// best effort: a data problem must never block a sign-in.
func TestTelegramBindingSurvivesClaimFailure(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	idp.signIn("subject-broken", "broken@example.com", "Пользователь")
	app.auth.Legacy = failingClaimer{}
	recorder := &claimRecorder{}
	app.auth.OnLegacyImported = recorder.record

	confirmTelegramBinding(t, app, idp, 279058397)

	if got := app.bindingCount(t); got != 1 {
		t.Fatalf("bindings = %d, want 1: a claim failure must not undo the binding", got)
	}
	if counts := recorder.counts(); len(counts) != 0 {
		t.Fatalf("OnLegacyImported calls = %v, want none", counts)
	}
}

// legacyReminderCount runs a counting query against the test database.
func (a *testApp) legacyReminderCount(t *testing.T, query string) int {
	t.Helper()
	var count int
	if err := a.store.Pool().QueryRow(t.Context(), query).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}
