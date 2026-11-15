package reminders_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/api"
	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// errNotUsed marks the identity methods the reminders tests never call.
var errNotUsed = errors.New("not used by the reminders tests")

// fakeAuth authenticates the HTTP tests: every bearer token maps to a
// principal, so a test can switch users without a real session flow.
type fakeAuth struct {
	tokens map[string]string
}

func (f fakeAuth) ResolveSession(_ context.Context, r *http.Request) (auth.Session, bool) {
	authorization := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || token == "" {
		return auth.Session{}, false
	}
	principal, known := f.tokens[token]
	if !known {
		return auth.Session{}, false
	}
	return auth.Session{PrincipalID: principal, Kind: auth.SessionKindBearer}, true
}

func (fakeAuth) Profile(context.Context, string) (auth.Profile, error) {
	return auth.Profile{}, errNotUsed
}

func (fakeAuth) UpdateTimezone(context.Context, string, string) (auth.Profile, error) {
	return auth.Profile{}, errNotUsed
}

func (fakeAuth) RevokeSession(context.Context, string) error { return nil }

func (fakeAuth) AuthenticateTelegram(context.Context, string) (auth.TelegramAuthResult, error) {
	return auth.TelegramAuthResult{}, errNotUsed
}

func (fakeAuth) ClearedSessionCookie() string { return "" }

func (fakeAuth) ListAPITokens(context.Context, string) ([]auth.APIToken, error) {
	return nil, errNotUsed
}

func (fakeAuth) CreateAPIToken(context.Context, string, string, []string, *int) (auth.APIToken, string, error) {
	return auth.APIToken{}, "", errNotUsed
}

func (fakeAuth) RevokeAPIToken(context.Context, string, string) error { return errNotUsed }

// apiRequest performs one JSON request and returns the status and the body.
func apiRequest(t *testing.T, method, url, token, body string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, url, err)
	}
	return response.StatusCode, data
}

// requireStatus fails when the response has another status.
func requireStatus(t *testing.T, got, want int, body []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, body)
	}
}

// decodeError decodes the shared error payload.
func decodeError(t *testing.T, body []byte) gen.Error {
	t.Helper()
	var payload gen.Error
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error payload %q: %v", body, err)
	}
	if payload.Message == "" {
		t.Errorf("error payload has no message: %s", body)
	}
	return payload
}

func TestHTTPRemindersAPI(t *testing.T) {
	env := newTestEnv(t)
	alice := env.createUser("alice")
	bob := env.createUser("bob")
	ctx := context.Background()

	server := httptest.NewServer(httpapi.New(httpapi.Deps{
		Version: "test",
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Spec:    api.OpenAPISpec,
		Auth: fakeAuth{tokens: map[string]string{
			"token-alice": alice,
			"token-bob":   bob,
		}},
		Reminders: env.service,
	}))
	t.Cleanup(server.Close)
	base := server.URL + "/api/v1"

	// Anonymous access is rejected with the shared payload.
	status, body := apiRequest(t, http.MethodGet, base+"/reminders", "", "")
	requireStatus(t, status, http.StatusUnauthorized, body)
	if code := decodeError(t, body).Code; code != "unauthorized" {
		t.Fatalf("error code = %q, want unauthorized", code)
	}

	// Invalid input is a bad request, and the message is the service one.
	invalid := `{"message":"","timezone":"Europe/Moscow","schedule_kind":"weekly","schedule_days":[1],"time":"09:00"}`
	status, body = apiRequest(t, http.MethodPost, base+"/reminders", "token-alice", invalid)
	requireStatus(t, status, http.StatusBadRequest, body)
	if code := decodeError(t, body).Code; code != "bad_request" {
		t.Fatalf("error code = %q, want bad_request", code)
	}

	// A valid reminder is created with the documented defaults.
	createdJSON := `{
		"message": "  Выпить таблетку  ",
		"timezone": "Europe/Moscow",
		"schedule_kind": "weekly",
		"schedule_days": [5, 1, 3, 1],
		"time": "12:00"
	}`
	status, body = apiRequest(t, http.MethodPost, base+"/reminders", "token-alice", createdJSON)
	requireStatus(t, status, http.StatusCreated, body)
	var created gen.Reminder
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created reminder %q: %v", body, err)
	}
	if created.Id == "" {
		t.Fatal("created reminder has no id")
	}
	if created.Message != "Выпить таблетку" {
		t.Errorf("message = %q, want the trimmed text", created.Message)
	}
	if len(created.ScheduleDays) != 3 || created.ScheduleDays[0] != 1 ||
		created.ScheduleDays[1] != 3 || created.ScheduleDays[2] != 5 {
		t.Errorf("schedule_days = %v, want [1 3 5]", created.ScheduleDays)
	}
	if created.RepeatMinutes != 10 {
		t.Errorf("repeat_minutes = %d, want the default 10", created.RepeatMinutes)
	}
	if !created.Enabled {
		t.Error("enabled = false, want the default true")
	}
	if created.NextDueAt == nil {
		t.Error("next_due_at is null for an enabled reminder")
	}
	if created.ActiveOccurrence != nil {
		t.Error("a new reminder has an active occurrence")
	}

	// List and get return the same reminder.
	status, body = apiRequest(t, http.MethodGet, base+"/reminders", "token-alice", "")
	requireStatus(t, status, http.StatusOK, body)
	var list []gen.Reminder
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode reminder list %q: %v", body, err)
	}
	if len(list) != 1 || list[0].Id != created.Id {
		t.Fatalf("list = %+v, want the created reminder", list)
	}

	itemURL := base + "/reminders/" + created.Id
	status, body = apiRequest(t, http.MethodGet, itemURL, "token-alice", "")
	requireStatus(t, status, http.StatusOK, body)

	// The replacement body is valid; only the owner is wrong.
	updatedJSON := `{
		"message": "Новое напоминание",
		"timezone": "Europe/Moscow",
		"schedule_kind": "monthly",
		"schedule_days": [15],
		"time": "10:00",
		"repeat_minutes": 30,
		"enabled": true
	}`

	// Another user cannot see or change the reminder, and the answer is the
	// same as for a reminder that does not exist.
	status, body = apiRequest(t, http.MethodGet, itemURL, "token-bob", "")
	requireStatus(t, status, http.StatusNotFound, body)
	if code := decodeError(t, body).Code; code != "not_found" {
		t.Fatalf("foreign get code = %q, want not_found", code)
	}
	status, body = apiRequest(t, http.MethodPut, itemURL, "token-bob", updatedJSON)
	requireStatus(t, status, http.StatusNotFound, body)
	status, body = apiRequest(t, http.MethodPost, itemURL+"/complete", "token-bob", "")
	requireStatus(t, status, http.StatusNotFound, body)
	status, body = apiRequest(t, http.MethodDelete, itemURL, "token-bob", "")
	requireStatus(t, status, http.StatusNotFound, body)

	// The owner replaces the rule.
	status, body = apiRequest(t, http.MethodPut, itemURL, "token-alice", updatedJSON)
	requireStatus(t, status, http.StatusOK, body)
	var updated gen.Reminder
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode updated reminder %q: %v", body, err)
	}
	if updated.Message != "Новое напоминание" || updated.ScheduleKind != gen.Monthly {
		t.Errorf("updated = %+v, want the replacement rule", updated)
	}
	if updated.RepeatMinutes != 30 {
		t.Errorf("repeat_minutes = %d, want 30", updated.RepeatMinutes)
	}

	// Nothing is pending yet.
	status, body = apiRequest(t, http.MethodPost, itemURL+"/complete", "token-alice", "")
	requireStatus(t, status, http.StatusConflict, body)
	if code := decodeError(t, body).Code; code != "conflict" {
		t.Fatalf("complete without occurrence code = %q, want conflict", code)
	}

	// Once the scheduler has fired the reminder, the API can confirm it.
	if updated.NextDueAt == nil {
		t.Fatal("updated reminder has no next_due_at")
	}
	occurrence := env.createOccurrence(created.Id, *updated.NextDueAt)
	status, body = apiRequest(t, http.MethodPost, itemURL+"/complete", "token-alice", "")
	requireStatus(t, status, http.StatusOK, body)
	var completed gen.Reminder
	if err := json.Unmarshal(body, &completed); err != nil {
		t.Fatalf("decode completed reminder %q: %v", body, err)
	}
	if completed.ActiveOccurrence != nil {
		t.Error("completed reminder still has an active occurrence")
	}
	if status := env.occurrenceStatus(occurrence.ID); status != "completed" {
		t.Fatalf("occurrence status = %q, want completed", status)
	}

	// Deleting removes the reminder; a second delete is a 404.
	status, body = apiRequest(t, http.MethodDelete, itemURL, "token-alice", "")
	requireStatus(t, status, http.StatusNoContent, body)
	status, body = apiRequest(t, http.MethodGet, itemURL, "token-alice", "")
	requireStatus(t, status, http.StatusNotFound, body)
	status, body = apiRequest(t, http.MethodDelete, itemURL, "token-alice", "")
	requireStatus(t, status, http.StatusNotFound, body)

	// The list is empty again.
	status, body = apiRequest(t, http.MethodGet, base+"/reminders", "token-alice", "")
	requireStatus(t, status, http.StatusOK, body)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode reminder list %q: %v", body, err)
	}
	if len(list) != 0 {
		t.Fatalf("list = %+v, want empty", list)
	}

	// The database really holds no reminders of Alice any more.
	var count int
	if err := env.store.Pool().QueryRow(ctx,
		"SELECT count(*) FROM reminders WHERE user_id = $1", parseTestUUID(t, alice),
	).Scan(&count); err != nil {
		t.Fatalf("count reminders: %v", err)
	}
	if count != 0 {
		t.Fatalf("reminders of Alice = %d, want 0", count)
	}
}
