package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/auth"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// TestOIDCLoginAndSessions covers M1-1 (users and bindings) and M1-3 (sessions,
// /me, logout) end to end against a real PostgreSQL and an in-process provider.
func TestOIDCLoginAndSessions(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	idp.signIn("subject-1", "user@example.com", "Иван Петров")

	client := app.newClient()

	// Anonymous API access is rejected with the shared error payload.
	resp, body := client.get("/api/v1/me")
	requireStatus(t, resp, http.StatusUnauthorized, body)
	if payload := decodeJSON[map[string]string](t, body); payload["code"] != "unauthorized" {
		t.Fatalf("error code = %q, want unauthorized; body: %s", payload["code"], body)
	}

	// The browser login starts at our page, not at the provider.
	resp, body = client.get("/auth/login?return_to=/app/")
	requireStatus(t, resp, http.StatusFound, body)
	authorizeURL := location(t, resp)
	if !strings.HasPrefix(authorizeURL, idp.issuer+"/authorize?") {
		t.Fatalf("login redirected to %q, want the provider", authorizeURL)
	}
	authorizeParams, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	query := authorizeParams.Query()
	if query.Get("state") == "" {
		t.Fatal("authorization request has no state")
	}
	if query.Get("nonce") == "" {
		t.Fatal("authorization request has no nonce")
	}
	if got := query.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", got)
	}

	// Provider round trip: authorize → callback.
	resp, body = client.do(http.MethodGet, authorizeURL, "", "", "")
	requireStatus(t, resp, http.StatusFound, body)
	callbackURL := location(t, resp)
	if !strings.HasPrefix(callbackURL, app.baseURL+"/auth/callback?") {
		t.Fatalf("callback URL = %q, want our /auth/callback", callbackURL)
	}
	resp, body = client.do(http.MethodGet, callbackURL, "", "", "")
	requireStatus(t, resp, http.StatusSeeOther, body)
	if got := location(t, resp); got != "/app/" {
		t.Fatalf("return_to target = %q, want /app/", got)
	}

	// The callback sets an HttpOnly, SameSite=Lax session cookie.
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("callback set no cookie")
	}
	cookie := cookies[0]
	if cookie.Name != auth.SessionCookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, auth.SessionCookieName)
	}
	if !cookie.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Secure {
		t.Error("Secure is set for an http public URL")
	}
	if cookie.Value == "" {
		t.Fatal("session cookie has no value")
	}

	// The session works.
	resp, body = client.get("/api/v1/me")
	requireStatus(t, resp, http.StatusOK, body)
	me := decodeJSON[meResponse](t, body)
	if me.ID == "" {
		t.Fatal("profile has no id")
	}
	if me.Email != "user@example.com" || me.DisplayName != "Иван Петров" {
		t.Errorf("profile = %+v, want the OIDC claims", me)
	}
	if me.Timezone != "UTC" {
		t.Errorf("timezone = %q, want the schema default UTC", me.Timezone)
	}
	if !strings.Contains(body, `"bindings":[]`) {
		t.Errorf("bindings must be an empty array, got: %s", body)
	}

	// State is single use: the same callback cannot sign in twice.
	resp, body = client.do(http.MethodGet, callbackURL, "", "", "")
	requireStatus(t, resp, http.StatusGone, body)

	// PATCH /me with JSON updates the time zone.
	resp, body = client.patchJSON("/api/v1/me", `{"timezone":"Europe/Moscow"}`, "")
	requireStatus(t, resp, http.StatusOK, body)
	if got := decodeJSON[meResponse](t, body).Timezone; got != "Europe/Moscow" {
		t.Fatalf("timezone = %q, want Europe/Moscow", got)
	}

	// An unknown time zone is a bad request and changes nothing.
	resp, body = client.patchJSON("/api/v1/me", `{"timezone":"Mars/Olympus"}`, "")
	requireStatus(t, resp, http.StatusBadRequest, body)

	// CSRF: a cookie session cannot send a form-encoded mutating request.
	resp, body = client.do(http.MethodPatch, app.baseURL+"/api/v1/me",
		"timezone=UTC", "application/x-www-form-urlencoded", "")
	requireStatus(t, resp, http.StatusForbidden, body)

	// The same PATCH through the Authorization header is not affected: only
	// cookie sessions need the JSON content type.
	session := app.seedSession(t, me.ID, auth.SessionKindBearer)
	resp, body = client.patchJSON("/api/v1/me", `{"timezone":"UTC"}`, session.Token)
	requireStatus(t, resp, http.StatusOK, body)

	// Logout revokes the token immediately.
	resp, body = client.do(http.MethodPost, app.baseURL+"/api/v1/auth/logout", `{}`, "application/json", session.Token)
	requireStatus(t, resp, http.StatusNoContent, body)
	if got := strings.Join(resp.Header.Values("Set-Cookie"), "; "); !strings.Contains(got, auth.SessionCookieName+"=") {
		t.Errorf("logout did not clear the cookie, Set-Cookie: %q", got)
	}
	resp, body = client.getWithBearer("/api/v1/me", session.Token)
	requireStatus(t, resp, http.StatusUnauthorized, body)

	// Logout is idempotent: no session, still 204.
	resp, body = client.postJSON("/api/v1/auth/logout", `{}`)
	requireStatus(t, resp, http.StatusNoContent, body)

	// return_to only accepts same-site relative paths: nothing is sent to the
	// provider for a rejected value.
	requestsBefore := idp.requestCount()
	for _, returnTo := range []string{
		"https://evil.example/",
		"//evil.example/",
		"/\\evil.example/",
		"javascript:alert(1)",
		"app/",
	} {
		resp, body = client.get("/auth/login?return_to=" + url.QueryEscape(returnTo))
		requireStatus(t, resp, http.StatusBadRequest, body)
	}
	if got := idp.requestCount(); got != requestsBefore {
		t.Errorf("provider saw %d authorization requests, want %d", got, requestsBefore)
	}

	// A relative path with a query string is accepted.
	resp, body = client.get("/auth/login?return_to=" + url.QueryEscape("/app/?tab=reminders"))
	requireStatus(t, resp, http.StatusFound, body)
}

// TestIdentityStore covers the messenger binding store of M1-1, including the
// re-binding rule: moving an account never duplicates it.
func TestIdentityStore(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	ctx := context.Background()

	alice := app.seedUser(t, "alice")
	bob := app.seedUser(t, "bob")
	if alice.ID == bob.ID {
		t.Fatal("different SSO subjects share a user row")
	}
	again := app.seedUser(t, "alice")
	if again.ID != alice.ID {
		t.Fatalf("UpsertFromOIDC created a second row: %q vs %q", again.ID, alice.ID)
	}

	bind := func(user auth.User, remoteID string) {
		t.Helper()
		if err := app.auth.Bind(ctx, unibot.Binding{
			PrincipalID:    unibot.PrincipalID(user.ID),
			Platform:       unibot.PlatformTelegram,
			RemoteUserID:   remoteID,
			ConversationID: remoteID,
		}); err != nil {
			t.Fatalf("Bind(%s → %s): %v", remoteID, user.ID, err)
		}
	}
	resolve := func(remoteID string) (string, bool) {
		t.Helper()
		principal, ok, err := app.auth.Resolve(ctx, unibot.PlatformTelegram, remoteID)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", remoteID, err)
		}
		return string(principal), ok
	}
	bindings := func(user auth.User) []unibot.Binding {
		t.Helper()
		items, err := app.auth.Bindings(ctx, unibot.PrincipalID(user.ID))
		if err != nil {
			t.Fatalf("Bindings(%s): %v", user.ID, err)
		}
		return items
	}

	bind(alice, "1001")
	if principal, ok := resolve("1001"); !ok || principal != alice.ID {
		t.Fatalf("Resolve = (%q, %v), want (%q, true)", principal, ok, alice.ID)
	}
	if got := bindings(alice); len(got) != 1 || got[0].RemoteUserID != "1001" || got[0].ConversationID != "1001" {
		t.Fatalf("alice bindings = %+v, want one telegram binding", got)
	}

	// Re-binding the same messenger account to another user moves the row.
	bind(bob, "1001")
	if principal, ok := resolve("1001"); !ok || principal != bob.ID {
		t.Fatalf("after rebinding Resolve = (%q, %v), want (%q, true)", principal, ok, bob.ID)
	}
	if got := bindings(alice); len(got) != 0 {
		t.Fatalf("alice still has bindings after the move: %+v", got)
	}
	if got := bindings(bob); len(got) != 1 {
		t.Fatalf("bob bindings = %+v, want exactly one", got)
	}

	var count int
	if err := app.store.Pool().QueryRow(ctx, `SELECT count(*) FROM messenger_bindings WHERE platform = 'telegram' AND remote_user_id = '1001'`).Scan(&count); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if count != 1 {
		t.Fatalf("telegram/1001 rows = %d, want 1", count)
	}

	// An unknown account is simply not linked.
	if _, ok := resolve("9999"); ok {
		t.Fatal("Resolve reported an unknown account as linked")
	}

	// The profile exposes the binding in the same shape as GET /me.
	profile, err := app.auth.Profile(ctx, bob.ID)
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if len(profile.Bindings) != 1 || profile.Bindings[0].Platform != "telegram" {
		t.Fatalf("profile bindings = %+v", profile.Bindings)
	}

	// UpsertFromOIDC never clears e-mail or display name of an existing row.
	email := "new@example.com"
	updated, err := app.auth.UpsertFromOIDC(ctx, auth.OIDCIdentity{
		Issuer:  idp.issuer,
		Subject: "bob",
		Email:   &email,
	})
	if err != nil {
		t.Fatalf("UpsertFromOIDC without a name: %v", err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "Пользователь bob" {
		t.Errorf("display name was cleared: %+v", updated)
	}
	if updated.Email == nil || *updated.Email != email {
		t.Errorf("email was not updated: %+v", updated)
	}
}

// TestMeRejectsUnknownBearer proves an unknown token is not a session.
func TestMeRejectsUnknownBearer(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)

	client := app.newClient()
	resp, body := client.getWithBearer("/api/v1/me", "not-a-real-token")
	requireStatus(t, resp, http.StatusUnauthorized, body)
}

// TestProfileJSONShape guards the generated schema mapping.
func TestProfileJSONShape(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	user := app.seedUser(t, "shape")
	if err := app.auth.Bind(context.Background(), unibot.Binding{
		PrincipalID:    unibot.PrincipalID(user.ID),
		Platform:       unibot.PlatformMatrix,
		RemoteUserID:   "@shape:example.com",
		ConversationID: "!room:example.com",
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	session := app.seedSession(t, user.ID, auth.SessionKindBearer)

	client := app.newClient()
	resp, body := client.getWithBearer("/api/v1/me", session.Token)
	requireStatus(t, resp, http.StatusOK, body)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"id", "timezone", "created_at", "bindings"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("profile misses %q: %s", field, body)
		}
	}
	me := decodeJSON[meResponse](t, body)
	if len(me.Bindings) != 1 || me.Bindings[0].Platform != "matrix" {
		t.Fatalf("bindings = %+v, want one matrix binding", me.Bindings)
	}
	if !strings.Contains(me.Bindings[0].VerifiedAt, "T") {
		t.Errorf("verified_at = %q, want RFC 3339", me.Bindings[0].VerifiedAt)
	}
}
