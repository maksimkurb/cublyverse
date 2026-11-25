package oauthserver_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/oauthserver"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

const issuer = "https://cv.example"

// fakeIdentity signs every request in as one user, or nobody.
type fakeIdentity struct{ user string }

func (f *fakeIdentity) ResolveSession(context.Context, *http.Request) (auth.Session, bool) {
	if f.user == "" {
		return auth.Session{}, false
	}
	return auth.Session{PrincipalID: f.user}, true
}

type env struct {
	t          *testing.T
	svc        *oauthserver.Service
	srv        *httptest.Server
	identity   *fakeIdentity
	workspaces *workspace.Service
	user, ws   string
}

func newEnv(t *testing.T, cfgMod func(*oauthserver.Config)) *env {
	t.Helper()
	ctx := context.Background()
	url := testdb.Start(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := store.Migrate(ctx, url, logger); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	name, email := "lilya", "l@example.com"
	u, err := st.Queries().CreateUser(ctx, db.CreateUserParams{OidcIssuer: "i", OidcSubject: "s", Email: &email, DisplayName: &name})
	if err != nil {
		t.Fatal(err)
	}
	userID := workspace.FormatID(u.ID)
	workspaces := workspace.New(st)
	ws, err := workspaces.Create(ctx, userID, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	cfg := oauthserver.Config{Issuer: issuer, Logger: logger}
	if cfgMod != nil {
		cfgMod(&cfg)
	}
	identity := &fakeIdentity{user: userID}
	svc := oauthserver.New(cfg, st, identity, workspaces)
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, svc: svc, srv: srv, identity: identity, workspaces: workspaces, user: userID, ws: ws.ID}
}

// noRedirect returns the response of a request without following redirects.
func (e *env) do(method, path string, form url.Values, cookies ...*http.Cookie) *http.Response {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (e *env) register(redirect string) string {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirect}, "client_name": "Claude", "token_endpoint_auth_method": "none"})
	resp, err := http.Post(e.srv.URL+"/oauth/register", "application/json", strings.NewReader(string(body)))
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("register status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.ClientID
}

func pkce() (verifier, challenge string) {
	verifier = strings.Repeat("a", 64)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorize runs the consent step and returns the code.
func (e *env) authorize(clientID, redirect, challenge, resource string) string {
	e.t.Helper()
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"st"},
		"scope": {"craft:read craft:write"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"resource": {resource},
	}
	page := e.do("GET", "/oauth/authorize?"+q.Encode(), nil)
	if page.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(page.Body)
		e.t.Fatalf("consent page status %d: %s", page.StatusCode, b)
	}
	var csrf *http.Cookie
	for _, c := range page.Cookies() {
		if c.Name == "cv_oauth_csrf" {
			csrf = c
		}
	}
	if csrf == nil {
		e.t.Fatal("no csrf cookie")
	}
	form := url.Values{}
	for k, v := range q {
		form[k] = v
	}
	form.Set("csrf", csrf.Value)
	form.Set("decision", "allow")
	form.Set("workspace_id", e.ws)
	resp := e.do("POST", "/oauth/authorize", form, csrf)
	if resp.StatusCode != http.StatusFound {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("decision status %d: %s", resp.StatusCode, b)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("state") != "st" || loc.Query().Get("iss") != issuer {
		e.t.Fatalf("bad redirect %s", loc)
	}
	return loc.Query().Get("code")
}

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (e *env) token(form url.Values) (int, tokens) {
	e.t.Helper()
	resp := e.do("POST", "/oauth/token", form)
	var out tokens
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAuthorizationCodeFlow(t *testing.T) {
	e := newEnv(t, nil)
	redirect := "http://127.0.0.1:33333/callback"
	clientID := e.register(redirect)
	verifier, challenge := pkce()
	resource := issuer + "/mcp/w/" + e.ws

	// A loopback client may use another port (RFC 8252).
	code := e.authorize(clientID, "http://127.0.0.1:44444/callback", challenge, resource)

	// Wrong verifier burns the code.
	status, out := e.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {"http://127.0.0.1:44444/callback"}, "code_verifier": {strings.Repeat("b", 64)}})
	if status != 400 || out.Error != "invalid_grant" {
		t.Fatalf("wrong verifier = %d %+v", status, out)
	}

	code = e.authorize(clientID, redirect, challenge, resource)
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier}}
	status, out = e.token(exchange)
	if status != 200 || out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatalf("exchange = %d %+v", status, out)
	}

	tok, err := e.svc.Verify(context.Background(), out.AccessToken)
	if err != nil || tok.WorkspaceID != e.ws || tok.UserID != e.user || tok.Resource != resource {
		t.Fatalf("Verify = %+v, %v", tok, err)
	}

	// Replaying the code revokes everything it produced.
	status, _ = e.token(exchange)
	if status != 400 {
		t.Fatalf("code replay status = %d, want 400", status)
	}
	if _, err := e.svc.Verify(context.Background(), out.AccessToken); err == nil {
		t.Fatal("access token must be revoked after a code replay")
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	e := newEnv(t, nil)
	redirect := "http://localhost:1/cb"
	clientID := e.register(redirect)
	verifier, challenge := pkce()
	code := e.authorize(clientID, redirect, challenge, issuer+"/mcp/w/"+e.ws)
	_, first := e.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier}})

	status, second := e.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {first.RefreshToken}})
	if status != 200 || second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh = %d %+v", status, second)
	}
	if _, err := e.svc.Verify(context.Background(), second.AccessToken); err != nil {
		t.Fatalf("new access token: %v", err)
	}
	// Reusing the spent refresh token revokes the whole family.
	if status, _ = e.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {first.RefreshToken}}); status != 400 {
		t.Fatalf("reuse status = %d", status)
	}
	if _, err := e.svc.Verify(context.Background(), second.AccessToken); err == nil {
		t.Fatal("family must be revoked after refresh reuse")
	}
}

func TestAuthorizeRejections(t *testing.T) {
	e := newEnv(t, nil)
	clientID := e.register("https://claude.example/callback")
	_, challenge := pkce()
	base := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://claude.example/callback"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}}

	cases := map[string]func(url.Values){
		"foreign redirect": func(v url.Values) { v.Set("redirect_uri", "https://evil.example/cb") },
		"unknown client":   func(v url.Values) { v.Set("client_id", "nope") },
	}
	for name, mod := range cases {
		v := url.Values{}
		for k, vs := range base {
			v[k] = vs
		}
		mod(v)
		if resp := e.do("GET", "/oauth/authorize?"+v.Encode(), nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 page without redirect", name, resp.StatusCode)
		}
	}

	// Missing PKCE goes back to the client as an error redirect.
	v := url.Values{}
	for k, vs := range base {
		v[k] = vs
	}
	v.Del("code_challenge")
	resp := e.do("GET", "/oauth/authorize?"+v.Encode(), nil)
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "error=invalid_request") {
		t.Errorf("no PKCE: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Signed out: bounce through the SSO login.
	e.identity.user = ""
	resp = e.do("GET", "/oauth/authorize?"+base.Encode(), nil)
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/auth/login?return_to=") {
		t.Errorf("signed out: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// CSRF: a decision without the cookie is refused.
	e.identity.user = e.user
	form := url.Values{"csrf": {"x"}, "decision": {"allow"}}
	for k, vs := range base {
		form[k] = vs
	}
	if resp := e.do("POST", "/oauth/authorize", form); resp.StatusCode != http.StatusForbidden {
		t.Errorf("csrf: status %d, want 403", resp.StatusCode)
	}
}

func TestClientIDMetadataDocument(t *testing.T) {
	var doc map[string]any
	meta := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(doc)
	}))
	defer meta.Close()
	clientID := meta.URL + "/client.json"
	doc = map[string]any{"client_id": clientID, "client_name": "CIMD App", "redirect_uris": []string{"https://app.example/cb"}}

	e := newEnv(t, func(c *oauthserver.Config) { c.HTTPClient = meta.Client() })
	_, challenge := pkce()
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://app.example/cb"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	resp := e.do("GET", "/oauth/authorize?"+q.Encode(), nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "CIMD App") {
		t.Fatalf("cimd consent: %d %s", resp.StatusCode, body)
	}

	// A document that names a different client_id is rejected.
	doc["client_id"] = "https://other.example/x"
	e2 := newEnv(t, func(c *oauthserver.Config) { c.HTTPClient = meta.Client() })
	if resp := e2.do("GET", "/oauth/authorize?"+q.Encode(), nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("mismatching client_id: status %d, want 400", resp.StatusCode)
	}
}

func TestMetadataDocuments(t *testing.T) {
	e := newEnv(t, nil)
	get := func(path string) (int, map[string]any) {
		resp := e.do("GET", path, nil)
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	status, m := get("/.well-known/oauth-authorization-server")
	if status != 200 || m["issuer"] != issuer || m["client_id_metadata_document_supported"] != true {
		t.Fatalf("AS metadata = %d %v", status, m)
	}
	status, m = get("/.well-known/oauth-protected-resource/mcp/w/" + e.ws)
	if status != 200 || m["resource"] != issuer+"/mcp/w/"+e.ws {
		t.Fatalf("resource metadata = %d %v", status, m)
	}
	if status, _ = get("/.well-known/oauth-protected-resource/other"); status != 404 {
		t.Fatalf("foreign resource path status = %d, want 404", status)
	}
}
