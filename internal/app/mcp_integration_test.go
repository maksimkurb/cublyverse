package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/app"
	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

// bearerTransport attaches a personal access token to every MCP request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

// mcpTestApp is a fully wired process behind httptest, which is what a real MCP
// client sees.
type mcpTestApp struct {
	t       *testing.T
	app     *app.App
	server  *httptest.Server
	user    auth.User
	session auth.Session
}

func newMCPTestApp(t *testing.T) *mcpTestApp {
	t.Helper()
	databaseURL := testdb.Start(t)
	cfg := config.Config{
		HTTPAddr:    freeAddr(t),
		DatabaseURL: databaseURL,
		LogFormat:   config.LogFormatText,
		// Enables the OAuth authorization server of the MCP endpoint.
		PublicBaseURL: "https://cv.example",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	application, err := app.New(context.Background(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(application.Close)

	server := httptest.NewServer(application.Handler())
	t.Cleanup(server.Close)

	user, err := application.Auth().UpsertFromOIDC(context.Background(), auth.OIDCIdentity{
		Issuer:  "https://idp.example.com",
		Subject: "mcp-user",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	session, err := application.Auth().CreateSession(context.Background(), user.ID, auth.SessionKindBearer)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &mcpTestApp{t: t, app: application, server: server, user: user, session: session}
}

// issueToken creates a PAT with the given scopes.
func (a *mcpTestApp) issueToken(scopes ...string) string {
	a.t.Helper()
	_, plaintext, err := a.app.Auth().CreateAPIToken(context.Background(), a.user.ID, "mcp-test", scopes, nil)
	if err != nil {
		a.t.Fatalf("create token: %v", err)
	}
	return plaintext
}

// createUser inserts another SSO user, the way a second person would sign in.
func (a *mcpTestApp) createUser(subject string) auth.User {
	a.t.Helper()
	user, err := a.app.Auth().UpsertFromOIDC(context.Background(), auth.OIDCIdentity{
		Issuer:  "https://idp.example.com",
		Subject: subject,
	})
	if err != nil {
		a.t.Fatalf("create user %q: %v", subject, err)
	}
	return user
}

// issueTokenFor creates a PAT of the given user.
func (a *mcpTestApp) issueTokenFor(user auth.User, scopes ...string) string {
	a.t.Helper()
	_, plaintext, err := a.app.Auth().CreateAPIToken(context.Background(), user.ID, "mcp-test", scopes, nil)
	if err != nil {
		a.t.Fatalf("create token: %v", err)
	}
	return plaintext
}

// connect opens a real Streamable HTTP MCP session.
func (a *mcpTestApp) connect(token string) *mcp.ClientSession {
	a.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "cublyverse-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             a.server.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		a.t.Fatalf("connect MCP client: %v", err)
	}
	a.t.Cleanup(func() { _ = session.Close() })
	return session
}

// api performs a plain HTTP API request with an optional bearer token.
func (a *mcpTestApp) api(method, path, bearer, body string) (int, string) {
	a.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, a.server.URL+"/api/v1"+path, reader)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		a.t.Fatalf("read %s %s: %v", method, path, err)
	}
	return response.StatusCode, string(payload)
}

// TestMCPOverStreamableHTTP is the end-to-end MCP test: a real Streamable HTTP
// client talks to /mcp with a personal access token, the token resolves to the
// user and the reminder tools write through the shared service.
func TestMCPOverStreamableHTTP(t *testing.T) {
	environment := newMCPTestApp(t)
	token := environment.issueToken(auth.ScopeProfileRead, auth.ScopeRemindersRead, auth.ScopeRemindersWrite)
	session := environment.connect(token)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{
		"whoami", "reminders_list", "reminders_get", "reminders_create",
		"reminders_update", "reminders_delete", "reminders_complete",
	} {
		if !containsString(names, want) {
			t.Errorf("tool %q is missing; got %v", want, names)
		}
	}

	who, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if who.IsError {
		t.Fatalf("whoami returned an error: %s", toolText(who))
	}
	identity := structured(t, who)
	if identity["principal_id"] != environment.user.ID {
		t.Errorf("whoami principal = %v, want %s", identity["principal_id"], environment.user.ID)
	}

	created, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "reminders_create",
		Arguments: map[string]any{
			"message":  "Проверка MCP",
			"weekdays": []string{"fri"},
			"time":     "18:00",
			"timezone": "Europe/Moscow",
		},
	})
	if err != nil {
		t.Fatalf("reminders_create: %v", err)
	}
	if created.IsError {
		t.Fatalf("reminders_create returned an error: %s", toolText(created))
	}
	reminderID, _ := structured(t, created)["id"].(string)
	if reminderID == "" {
		t.Fatal("reminders_create returned no id")
	}

	// The same reminder is visible through the HTTP API with the same token.
	status, body := environment.api(http.MethodGet, "/reminders", token, "")
	if status != http.StatusOK {
		t.Fatalf("GET /reminders = %d, want 200; body: %s", status, body)
	}
	if !strings.Contains(body, reminderID) {
		t.Errorf("GET /reminders does not contain the reminder created over MCP: %s", body)
	}

	listed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reminders_list"})
	if err != nil {
		t.Fatalf("reminders_list: %v", err)
	}
	if listed.IsError {
		t.Fatalf("reminders_list returned an error: %s", toolText(listed))
	}
	if !strings.Contains(toolText(listed), "Проверка MCP") {
		t.Errorf("reminders_list does not show the reminder: %s", toolText(listed))
	}

	// A token that last used the reminder must have recorded the use.
	lastUsed, err := environment.app.Auth().ListAPITokens(context.Background(), environment.user.ID)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(lastUsed) == 0 || lastUsed[0].LastUsedAt == nil {
		t.Error("last_used_at was not recorded when the token authenticated /mcp")
	}
}

// TestMCPHTTPAuth covers the token rejections over the real endpoint.
func TestMCPHTTPAuth(t *testing.T) {
	environment := newMCPTestApp(t)

	// No token at all, and a token that does not exist.
	for name, token := range map[string]string{
		"no token":      "",
		"unknown token": "cv_pat_" + strings.Repeat("B", 43),
	} {
		t.Run(name, func(t *testing.T) {
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			transport := &mcp.StreamableClientTransport{
				Endpoint:             environment.server.URL + "/mcp",
				HTTPClient:           &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}},
				DisableStandaloneSSE: true,
				MaxRetries:           -1,
			}
			if _, err := client.Connect(context.Background(), transport, nil); err == nil {
				t.Fatal("connect succeeded with a token that must be rejected")
			}
		})
	}

	// A revoked token.
	revoked := environment.issueToken(auth.ScopeRemindersRead)
	tokens, err := environment.app.Auth().ListAPITokens(context.Background(), environment.user.ID)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if err := environment.app.Auth().RevokeAPIToken(context.Background(), environment.user.ID, tokens[0].ID); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             environment.server.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: revoked, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	if _, err := client.Connect(context.Background(), transport, nil); err == nil {
		t.Fatal("connect succeeded with a revoked token")
	}

	// A cookie session is not accepted by /mcp: the endpoint is PAT only.
	request, err := http.NewRequest(http.MethodPost, environment.server.URL+"/mcp", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: environment.session.Token})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST /mcp with a cookie: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /mcp with a cookie session = %d, want 401", response.StatusCode)
	}
}

// TestPATScopeEnforcementHTTP checks the scope and session-only rules through
// the HTTP API, where the middleware maps a request to a scope.
func TestPATScopeEnforcementHTTP(t *testing.T) {
	environment := newMCPTestApp(t)
	readOnly := environment.issueToken(auth.ScopeRemindersRead)

	if status, body := environment.api(http.MethodGet, "/me", readOnly, ""); status != http.StatusOK {
		t.Fatalf("GET /me with a read token = %d, want 200; body: %s", status, body)
	}
	if status, body := environment.api(http.MethodGet, "/reminders", readOnly, ""); status != http.StatusOK {
		t.Fatalf("GET /reminders with reminders:read = %d, want 200; body: %s", status, body)
	}
	if status, body := environment.api(http.MethodPost, "/reminders", readOnly,
		`{"message":"x","timezone":"UTC","schedule_kind":"weekly","schedule_days":[1],"time":"09:00"}`); status != http.StatusForbidden {
		t.Fatalf("POST /reminders with reminders:read = %d, want 403; body: %s", status, body)
	}
	if status, body := environment.api(http.MethodPatch, "/me", readOnly, `{"timezone":"Europe/Moscow"}`); status != http.StatusForbidden {
		t.Fatalf("PATCH /me with a token = %d, want 403; body: %s", status, body)
	}
	if status, body := environment.api(http.MethodPost, "/tokens", environment.session.Token,
		`{"name":"sneaky","scopes":["reminders:write"]}`); status != http.StatusCreated {
		t.Fatalf("POST /tokens with a session = %d, want 201; body: %s", status, body)
	}

	// A token with a read scope may call whoami but not a write tool.
	readSession := environment.connect(readOnly)
	result, err := readSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "reminders_create",
		Arguments: map[string]any{"message": "x", "weekdays": []string{"mon"}, "time": "09:00"},
	})
	if err != nil {
		t.Fatalf("reminders_create: %v", err)
	}
	if !result.IsError || !strings.Contains(toolText(result), "insufficient scope") {
		t.Fatalf("reminders_create with a read token = %s, want an insufficient scope error", toolText(result))
	}
}

// TestMCPTokenOwnershipIsolation is the two-users' PATs case over the real
// endpoint: a token reaches only the reminders of the user it belongs to.
func TestMCPTokenOwnershipIsolation(t *testing.T) {
	environment := newMCPTestApp(t)
	aliceToken := environment.issueToken(auth.ScopeRemindersRead, auth.ScopeRemindersWrite)
	bob := environment.createUser("bob")
	bobToken := environment.issueTokenFor(bob, auth.ScopeRemindersRead, auth.ScopeRemindersWrite)

	aliceSession := environment.connect(aliceToken)
	created, err := aliceSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "reminders_create",
		Arguments: map[string]any{
			"message":    "Только для Алисы",
			"month_days": []int{15},
			"time":       "12:00",
			"timezone":   "Europe/Moscow",
		},
	})
	if err != nil {
		t.Fatalf("reminders_create: %v", err)
	}
	if created.IsError {
		t.Fatalf("reminders_create returned an error: %s", toolText(created))
	}
	reminderID, _ := structured(t, created)["id"].(string)
	if reminderID == "" {
		t.Fatal("reminders_create returned no id")
	}

	bobSession := environment.connect(bobToken)
	listed, err := bobSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "reminders_list"})
	if err != nil {
		t.Fatalf("bob reminders_list: %v", err)
	}
	if strings.Contains(toolText(listed), "Только для Алисы") {
		t.Errorf("bob sees Alice's reminder over MCP: %s", toolText(listed))
	}

	for _, tool := range []string{"reminders_get", "reminders_delete", "reminders_complete"} {
		result, err := bobSession.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      tool,
			Arguments: map[string]any{"id": reminderID},
		})
		if err != nil {
			t.Fatalf("bob %s: %v", tool, err)
		}
		if !result.IsError || !strings.Contains(toolText(result), "not found") {
			t.Errorf("bob %s = %s, want a not found error", tool, toolText(result))
		}
	}

	// Through the API the same answers: 404, never the object.
	if status, body := environment.api(http.MethodGet, "/reminders/"+reminderID, bobToken, ""); status != http.StatusNotFound {
		t.Errorf("GET /reminders/%s with bob's token = %d, want 404; body: %s", reminderID, status, body)
	}
	if status, body := environment.api(http.MethodGet, "/reminders/"+reminderID, aliceToken, ""); status != http.StatusOK {
		t.Errorf("GET /reminders/%s with alice's token = %d, want 200; body: %s", reminderID, status, body)
	}
}

func structured(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode structured content %s: %v", encoded, err)
	}
	return decoded
}

func toolText(result *mcp.CallToolResult) string {
	var parts []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
