package auth_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/api"
	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/httpapi"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testApp is a real service stack: PostgreSQL container, the identity service
// and the complete HTTP handler (API plus /auth pages) behind httptest.
type testApp struct {
	t       *testing.T
	store   *store.Store
	auth    *auth.Service
	server  *httptest.Server
	baseURL string
	idp     *fakeIDP

	mu     sync.Mutex
	linked []unibot.Binding
}

// newTestApp starts PostgreSQL, applies the migrations and wires the HTTP
// handler. tweak may adjust the auth configuration (for example shorten a TTL).
func newTestApp(t *testing.T, idp *fakeIDP, tweak func(*auth.Config)) *testApp {
	t.Helper()

	databaseURL := testdb.Start(t)
	ctx := context.Background()
	logger := testLogger()
	if err := store.Migrate(ctx, databaseURL, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	// The redirect URI has to match the httptest server, so the listener is
	// created first and the handler is attached right before Start.
	server := httptest.NewUnstartedServer(nil)
	baseURL := "http://" + server.Listener.Addr().String()

	cfg := auth.Config{
		PublicBaseURL:    baseURL,
		TelegramBotToken: testBotToken,
		OIDC: auth.OIDCConfig{
			Issuer:       idp.issuer,
			ClientID:     testClientID,
			ClientSecret: testClientSecret,
			RedirectURL:  baseURL + "/auth/callback",
		},
	}
	if tweak != nil {
		tweak(&cfg)
	}
	service, err := auth.New(ctx, st, cfg, logger)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}

	app := &testApp{t: t, store: st, auth: service, idp: idp}
	service.OnLinked = func(_ context.Context, binding unibot.Binding) {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.linked = append(app.linked, binding)
	}

	server.Config.Handler = httpapi.New(httpapi.Deps{
		Version:   "test",
		Logger:    logger,
		Ready:     st.Ping,
		Spec:      api.OpenAPISpec,
		Auth:      service,
		AuthPages: service.Handler(),
	})
	server.Start()
	t.Cleanup(server.Close)

	app.server = server
	app.baseURL = baseURL
	return app
}

// linkedBindings returns the bindings reported through OnLinked.
func (a *testApp) linkedBindings() []unibot.Binding {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]unibot.Binding(nil), a.linked...)
}

// seedUser creates a user row the way an OIDC sign-in would.
func (a *testApp) seedUser(t *testing.T, subject string) auth.User {
	t.Helper()
	email := subject + "@example.com"
	name := "Пользователь " + subject
	user, err := a.auth.UpsertFromOIDC(context.Background(), auth.OIDCIdentity{
		Issuer:      a.idp.issuer,
		Subject:     subject,
		Email:       &email,
		DisplayName: &name,
	})
	if err != nil {
		t.Fatalf("UpsertFromOIDC(%q): %v", subject, err)
	}
	return user
}

// seedSession creates a session of the given kind and returns its token.
func (a *testApp) seedSession(t *testing.T, principalID, kind string) auth.Session {
	t.Helper()
	session, err := a.auth.CreateSession(context.Background(), principalID, kind)
	if err != nil {
		t.Fatalf("CreateSession(%s): %v", kind, err)
	}
	return session
}

// testClient walks redirects manually and keeps cookies, which is what a browser
// does — minus the automatic following, so each step can be inspected.
type testClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func (a *testApp) newClient() *testClient {
	a.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		a.t.Fatalf("cookie jar: %v", err)
	}
	return &testClient{
		t:    a.t,
		base: a.baseURL,
		http: &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// do performs one request. url is used as-is, so it can point at the fake
// identity provider as well.
func (c *testClient) do(method, url string, body string, contentType, bearer string) (*http.Response, string) {
	c.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, url, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read %s %s: %v", method, url, err)
	}
	return resp, string(data)
}

// get requests a path of the service under test.
func (c *testClient) get(path string) (*http.Response, string) {
	c.t.Helper()
	return c.do(http.MethodGet, c.base+path, "", "", "")
}

// fetch follows a target that may be absolute or a path of the test server.
func (c *testClient) fetch(target string) (*http.Response, string) {
	c.t.Helper()
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return c.do(http.MethodGet, target, "", "", "")
	}
	return c.get(target)
}

func (c *testClient) getWithBearer(path, bearer string) (*http.Response, string) {
	c.t.Helper()
	return c.do(http.MethodGet, c.base+path, "", "", bearer)
}

func (c *testClient) postJSON(path, body string) (*http.Response, string) {
	c.t.Helper()
	return c.do(http.MethodPost, c.base+path, body, "application/json", "")
}

func (c *testClient) postForm(path string, values url.Values) (*http.Response, string) {
	c.t.Helper()
	return c.do(http.MethodPost, c.base+path, values.Encode(), "application/x-www-form-urlencoded", "")
}

func (c *testClient) patchJSON(path, body, bearer string) (*http.Response, string) {
	c.t.Helper()
	return c.do(http.MethodPatch, c.base+path, body, "application/json", bearer)
}

// location returns the raw Location header of a redirect response.
func location(t *testing.T, resp *http.Response) string {
	t.Helper()
	target := resp.Header.Get("Location")
	if target == "" {
		t.Fatalf("response %d has no Location header", resp.StatusCode)
	}
	return target
}

// requireStatus fails when the response has another status.
func requireStatus(t *testing.T, resp *http.Response, want int, body string) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, body)
	}
}

// decodeJSON unmarshals a response body.
func decodeJSON[T any](t *testing.T, body string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return value
}

// formToken extracts the confirmation token from the confirmation page.
func formToken(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="token" value="`
	index := strings.Index(body, marker)
	if index < 0 {
		t.Fatalf("confirmation page has no token field:\n%s", body)
	}
	rest := body[index+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("confirmation page token is malformed")
	}
	return rest[:end]
}

// initData builds a correctly signed Telegram Mini App payload.
func initData(t *testing.T, userID int64, authDate time.Time) string {
	t.Helper()
	return initDataWithToken(t, testBotToken, userID, authDate)
}

// initDataWithToken signs an initData payload with an arbitrary token, which is
// how a forged payload is built.
func initDataWithToken(t *testing.T, botToken string, userID int64, authDate time.Time) string {
	t.Helper()
	values := url.Values{}
	values.Set("query_id", "AAH-test")
	values.Set("user", fmt.Sprintf(
		`{"id":%d,"first_name":"Тест","last_name":"Пользователь","username":"tester"}`,
		userID,
	))
	values.Set("auth_date", strconv.FormatInt(authDate.Unix(), 10))

	// Telegram signs the decoded key=value pairs sorted by key, joined by "\n".
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	checkString := strings.Join(parts, "\n")

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	secret := secretMAC.Sum(nil)
	dataMAC := hmac.New(sha256.New, secret)
	_, _ = dataMAC.Write([]byte(checkString))
	values.Set("hash", hex.EncodeToString(dataMAC.Sum(nil)))
	return values.Encode()
}

// telegramLoginResponse mirrors the API schema for the assertions below.
type telegramLoginResponse struct {
	Status    string      `json:"status"`
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      *meResponse `json:"user"`
	LinkURL   string      `json:"link_url"`
}

type meResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Timezone    string `json:"timezone"`
	CreatedAt   string `json:"created_at"`
	Bindings    []struct {
		Platform       string `json:"platform"`
		RemoteUserID   string `json:"remote_user_id"`
		ConversationID string `json:"conversation_id"`
		VerifiedAt     string `json:"verified_at"`
	} `json:"bindings"`
}

// telegramLink returns the link URL of an unbound Telegram account.
func (a *testApp) telegramLink(t *testing.T) (string, string) {
	t.Helper()
	client := a.newClient()
	resp, body := client.postJSON("/api/v1/auth/telegram", fmt.Sprintf(
		`{"init_data":%q}`, initData(t, 279058397, time.Now()),
	))
	requireStatus(t, resp, http.StatusOK, body)
	payload := decodeJSON[telegramLoginResponse](t, body)
	if payload.Status != "link_required" {
		t.Fatalf("status = %q, want link_required; body: %s", payload.Status, body)
	}
	if payload.LinkURL == "" {
		t.Fatal("link_url is empty")
	}
	parsed, err := url.Parse(payload.LinkURL)
	if err != nil {
		t.Fatalf("parse link_url %q: %v", payload.LinkURL, err)
	}
	return payload.LinkURL, parsed.Query().Get("state")
}
