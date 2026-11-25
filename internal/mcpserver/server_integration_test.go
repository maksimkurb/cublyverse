package mcpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

// testEnv is a real service stack for MCP tests: PostgreSQL with migrations, the
// identity service (for profiles and users) and the reminders server with the
// feature tools registered exactly as internal/app does it.
type testEnv struct {
	t      *testing.T
	store  *store.Store
	auth   *auth.Service
	server *mcpserver.Server
	logger *slog.Logger
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	databaseURL := testdb.Start(t)
	if err := store.Migrate(ctx, databaseURL, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	authService, err := auth.New(ctx, st, auth.Config{}, logger)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}

	service := reminders.New(st, reminders.WithLogger(logger))
	env := &testEnv{t: t, store: st, auth: authService, logger: logger}
	env.server = mcpserver.New(mcpserver.Deps{
		Version: "test",
		Logger:  logger,
		Profile: authService.Profile,
	})
	reminders.NewFeature(service, logger,
		reminders.WithTimezoneLookup(func(ctx context.Context, principalID string) (string, error) {
			profile, err := authService.Profile(ctx, principalID)
			if err != nil {
				return "", err
			}
			return profile.User.Timezone, nil
		}),
	).RegisterMCP(env.server)
	return env
}

// createUser inserts a user the way an OIDC sign-in would.
func (e *testEnv) createUser(subject string) auth.User {
	e.t.Helper()
	email := subject + "@example.com"
	name := "Пользователь " + subject
	user, err := e.auth.UpsertFromOIDC(context.Background(), auth.OIDCIdentity{
		Issuer:      "https://idp.example.com",
		Subject:     subject,
		Email:       &email,
		DisplayName: &name,
	})
	if err != nil {
		e.t.Fatalf("create user %q: %v", subject, err)
	}
	return user
}

// principal builds the identity Handler would put into the context for a token.
func principal(id string, scopes ...string) mcpserver.Principal {
	return mcpserver.Principal{ID: id, Scopes: scopes, TokenID: "test-token"}
}

// connect attaches an in-memory MCP client with the given principal. The
// connection context is what every tool of that session sees, mirroring the
// HTTP handler that stores the authenticated principal in the request context.
func (e *testEnv) connect(identity mcpserver.Principal) *mcp.ClientSession {
	e.t.Helper()
	ctx := mcpserver.WithPrincipal(context.Background(), identity)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := e.server.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		e.t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		e.t.Fatalf("connect client: %v", err)
	}
	e.t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession
}

// call invokes a tool and returns its structured content or its error text.
func call(t *testing.T, session *mcp.ClientSession, name string, arguments any) (map[string]any, string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.IsError {
		return nil, contentText(result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal structured content: %v", name, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("%s: decode structured content %s: %v", name, encoded, err)
	}
	return decoded, ""
}

func contentText(result *mcp.CallToolResult) string {
	var parts []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func requireString(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	raw, ok := value[key]
	if !ok {
		t.Fatalf("%q is missing in %v", key, value)
	}
	text, ok := raw.(string)
	if !ok {
		t.Fatalf("%q is %T, want string", key, raw)
	}
	return text
}

// TestReminderTools covers the whole tool surface against a real service:
// create, list, get, update, complete and delete.
func TestReminderTools(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser("tools")
	session := env.connect(principal(user.ID, auth.ScopeRemindersRead, auth.ScopeRemindersWrite))

	created, errText := call(t, session, "reminders_create", map[string]any{
		"message":  "Выпить таблетку",
		"weekdays": []string{"mon", "wed"},
		"time":     "09:30",
		"timezone": "Europe/Moscow",
	})
	if errText != "" {
		t.Fatalf("create: %s", errText)
	}
	id := requireString(t, created, "id")
	if kind := requireString(t, created, "schedule_kind"); kind != "weekly" {
		t.Errorf("schedule_kind = %q, want weekly", kind)
	}
	if timeOfDay := requireString(t, created, "time"); timeOfDay != "09:30" {
		t.Errorf("time = %q, want 09:30", timeOfDay)
	}
	if tz := requireString(t, created, "time_zone"); tz != "Europe/Moscow" {
		t.Errorf("time_zone = %q, want Europe/Moscow", tz)
	}
	weekdays, ok := created["weekdays"].([]any)
	if !ok || len(weekdays) != 2 || weekdays[0] != "mon" || weekdays[1] != "wed" {
		t.Errorf("weekdays = %v, want [mon wed]", created["weekdays"])
	}
	if next := requireString(t, created, "next_due_at"); next == "" {
		t.Error("next_due_at is empty for an enabled reminder")
	}

	listed, errText := call(t, session, "reminders_list", map[string]any{})
	if errText != "" {
		t.Fatalf("list: %s", errText)
	}
	remindersList, ok := listed["reminders"].([]any)
	if !ok || len(remindersList) != 1 {
		t.Fatalf("list returned %v, want one reminder", listed["reminders"])
	}

	fetched, errText := call(t, session, "reminders_get", map[string]any{"id": id})
	if errText != "" {
		t.Fatalf("get: %s", errText)
	}
	if requireString(t, fetched, "message") != "Выпить таблетку" {
		t.Errorf("get returned another reminder: %v", fetched)
	}

	updated, errText := call(t, session, "reminders_update", map[string]any{
		"id":       id,
		"time":     "10:15",
		"weekdays": []string{"2", "4"},
	})
	if errText != "" {
		t.Fatalf("update: %s", errText)
	}
	if requireString(t, updated, "time") != "10:15" {
		t.Errorf("update did not change the time: %v", updated)
	}
	if requireString(t, updated, "message") != "Выпить таблетку" {
		t.Errorf("update lost the message: %v", updated)
	}
	if days := updated["weekdays"].([]any); len(days) != 2 || days[0] != "tue" || days[1] != "thu" {
		t.Errorf("weekdays = %v, want [tue thu]", updated["weekdays"])
	}

	// Nothing has fired yet, so there is nothing to confirm.
	if _, errText := call(t, session, "reminders_complete", map[string]any{"id": id}); !strings.Contains(errText, "nothing to confirm") {
		t.Fatalf("complete before firing = %q, want a 'nothing to confirm' tool error", errText)
	}

	// Simulate a firing the way the scheduler does. The service passes its
	// insert to sqlc; the test inserts the active occurrence directly.
	createOccurrence(t, env, id)

	completed, errText := call(t, session, "reminders_complete", map[string]any{"id": id})
	if errText != "" {
		t.Fatalf("complete: %s", errText)
	}
	if _, pending := completed["active_occurrence"]; pending {
		t.Errorf("completed reminder still reports a pending occurrence: %v", completed)
	}

	deleted, errText := call(t, session, "reminders_delete", map[string]any{"id": id})
	if errText != "" {
		t.Fatalf("delete: %s", errText)
	}
	if deleted["deleted"] != true {
		t.Errorf("delete answered %v", deleted)
	}

	if _, errText := call(t, session, "reminders_get", map[string]any{"id": id}); !strings.Contains(errText, "not found") {
		t.Fatalf("get after delete = %q, want a not found tool error", errText)
	}
}

// createOccurrence inserts the active occurrence of a reminder, which is what
// the scheduler does after delivering the first message.
func createOccurrence(t *testing.T, env *testEnv, reminderID string) {
	t.Helper()
	var id pgtype.UUID
	if err := env.store.Pool().QueryRow(context.Background(),
		"SELECT id FROM reminders WHERE id::text = $1", reminderID,
	).Scan(&id); err != nil {
		t.Fatalf("find reminder %s: %v", reminderID, err)
	}
	if _, err := env.store.Queries().CreateOccurrence(context.Background(), db.CreateOccurrenceParams{
		ReminderID:  id,
		ScheduledAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create occurrence: %v", err)
	}
}

// TestReminderToolsOwnershipIsolation proves that two users' tokens cannot see
// or touch each other's reminders.
func TestReminderToolsOwnershipIsolation(t *testing.T) {
	env := newTestEnv(t)
	alice := env.createUser("alice")
	bob := env.createUser("bob")
	aliceSession := env.connect(principal(alice.ID, auth.ScopeRemindersRead, auth.ScopeRemindersWrite))
	bobSession := env.connect(principal(bob.ID, auth.ScopeRemindersRead, auth.ScopeRemindersWrite))

	created, errText := call(t, aliceSession, "reminders_create", map[string]any{
		"message":    "Только для Алисы",
		"month_days": []int{15},
		"time":       "12:00",
		"timezone":   "Europe/Moscow",
	})
	if errText != "" {
		t.Fatalf("alice create: %s", errText)
	}
	id := requireString(t, created, "id")

	listed, errText := call(t, bobSession, "reminders_list", map[string]any{})
	if errText != "" {
		t.Fatalf("bob list: %s", errText)
	}
	if items := listed["reminders"].([]any); len(items) != 0 {
		t.Fatalf("bob sees %d reminders of alice", len(items))
	}

	for _, tool := range []string{"reminders_get", "reminders_delete", "reminders_complete"} {
		if _, errText := call(t, bobSession, tool, map[string]any{"id": id}); !strings.Contains(errText, "not found") {
			t.Errorf("%s of alice's reminder = %q, want a not found tool error", tool, errText)
		}
	}

	// The reminder survived and still belongs to Alice.
	if _, errText := call(t, aliceSession, "reminders_get", map[string]any{"id": id}); errText != "" {
		t.Fatalf("alice get after bob's attempts: %s", errText)
	}
}

// TestReminderToolsScopes proves that a read-only token cannot write.
func TestReminderToolsScopes(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser("readonly")
	readOnly := env.connect(principal(user.ID, auth.ScopeRemindersRead))

	if _, errText := call(t, readOnly, "reminders_list", map[string]any{}); errText != "" {
		t.Fatalf("list with a read-only token: %s", errText)
	}

	for _, invocation := range []struct {
		tool string
		args map[string]any
	}{
		{"reminders_create", map[string]any{"message": "x", "weekdays": []string{"mon"}, "time": "09:00"}},
		{"reminders_update", map[string]any{"id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c", "time": "10:00"}},
		{"reminders_delete", map[string]any{"id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"}},
		{"reminders_complete", map[string]any{"id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"}},
	} {
		if _, errText := call(t, readOnly, invocation.tool, invocation.args); !strings.Contains(errText, "insufficient scope") {
			t.Errorf("%s with a read-only token = %q, want an insufficient scope error", invocation.tool, errText)
		}
	}

	// whoami is allowed with any granted scope.
	identity, errText := call(t, readOnly, "whoami", map[string]any{})
	if errText != "" {
		t.Fatalf("whoami with a read-only token: %s", errText)
	}
	if requireString(t, identity, "principal_id") != user.ID {
		t.Errorf("whoami returned %v, want principal %s", identity, user.ID)
	}
	scopes, ok := identity["scopes"].([]any)
	if !ok || len(scopes) != 1 || scopes[0] != auth.ScopeRemindersRead {
		t.Errorf("whoami scopes = %v, want [reminders:read]", identity["scopes"])
	}
}

// TestReminderToolsDefaults checks the LLM-friendly defaults: the profile time
// zone, a ten-minute repeat and an enabled reminder.
func TestReminderToolsDefaults(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser("defaults")
	if _, err := env.auth.UpdateTimezone(context.Background(), user.ID, "Asia/Oral"); err != nil {
		t.Fatalf("set profile time zone: %v", err)
	}
	session := env.connect(principal(user.ID, auth.ScopeRemindersRead, auth.ScopeRemindersWrite))

	created, errText := call(t, session, "reminders_create", map[string]any{
		"message":  "Без часового пояса",
		"weekdays": []string{"1", "3", "5"},
		"time":     "08:00",
	})
	if errText != "" {
		t.Fatalf("create: %s", errText)
	}
	if tz := requireString(t, created, "time_zone"); tz != "Asia/Oral" {
		t.Errorf("time_zone = %q, want the profile time zone Asia/Oral", tz)
	}
	if repeat, ok := created["repeat_minutes"].(float64); !ok || repeat != 10 {
		t.Errorf("repeat_minutes = %v, want 10", created["repeat_minutes"])
	}
	if enabled, ok := created["enabled"].(bool); !ok || !enabled {
		t.Errorf("enabled = %v, want true", created["enabled"])
	}

	identity, errText := call(t, session, "whoami", map[string]any{})
	if errText != "" {
		t.Fatalf("whoami: %s", errText)
	}
	if tz := requireString(t, identity, "time_zone"); tz != "Asia/Oral" {
		t.Errorf("whoami time_zone = %q, want Asia/Oral", tz)
	}
	if email := requireString(t, identity, "email"); email != "defaults@example.com" {
		t.Errorf("whoami email = %q", email)
	}
}

// TestToolListSchema is the schema sanity test: every tool has a description and
// an object input schema, and the annotations match the intended access.
func TestToolListSchema(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser("schema")
	session := env.connect(principal(user.ID, auth.ScopeRemindersRead, auth.ScopeRemindersWrite))

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]struct {
		readOnly    bool
		destructive bool
	}{
		"whoami":             {readOnly: true},
		"spaces_list":        {readOnly: true},
		"reminders_list":     {readOnly: true},
		"reminders_get":      {readOnly: true},
		"reminders_create":   {destructive: false},
		"reminders_update":   {destructive: false},
		"reminders_delete":   {destructive: true},
		"reminders_complete": {destructive: false},
	}
	if len(result.Tools) != len(want) {
		t.Fatalf("tools = %d, want %d", len(result.Tools), len(want))
	}
	for _, tool := range result.Tools {
		expected, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		if tool.Annotations == nil {
			t.Errorf("%s has no annotations", tool.Name)
			continue
		}
		if tool.Annotations.ReadOnlyHint != expected.readOnly {
			t.Errorf("%s readOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, expected.readOnly)
		}
		if !expected.readOnly {
			destructive := tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint
			if destructive != expected.destructive {
				t.Errorf("%s destructiveHint = %v, want %v", tool.Name, destructive, expected.destructive)
			}
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("%s input schema is %T, want an object", tool.Name, tool.InputSchema)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("%s input schema type = %v, want object", tool.Name, schema["type"])
		}
		if tool.Name != "whoami" && tool.Name != "reminders_list" && tool.OutputSchema == nil {
			t.Errorf("%s has no output schema", tool.Name)
		}
	}
}
