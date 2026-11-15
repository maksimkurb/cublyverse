package auth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestTelegramSignInAndLinking is the M1-4 / M1-5 acceptance test: a Telegram
// account can only be bound after the confirmation step, the Mini App initData
// must belong to the pending account, and every state or token is single use.
func TestTelegramSignInAndLinking(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	idp.signIn("subject-tg", "tg@example.com", "Телеграм Пользователь")
	client := app.newClient()

	const telegramUserID = 279058397

	// A payload signed with another bot token is forged.
	forged := initDataWithToken(t, "111111:wrong-token", telegramUserID, time.Now())
	resp, body := client.postJSON("/api/v1/auth/telegram", fmt.Sprintf(`{"init_data":%q}`, forged))
	requireStatus(t, resp, http.StatusUnauthorized, body)
	if payload := decodeJSON[map[string]string](t, body); payload["code"] != "invalid_init_data" {
		t.Errorf("error code = %q, want invalid_init_data", payload["code"])
	}

	// An unbound account is asked to link first.
	linkURL, state := app.telegramLink(t)
	if state == "" {
		t.Fatal("link_url carries no state")
	}
	link, err := url.Parse(linkURL)
	if err != nil {
		t.Fatalf("parse link_url %q: %v", linkURL, err)
	}
	if link.Path != "/auth/login" {
		t.Fatalf("link_url path = %q, want /auth/login", link.Path)
	}
	if link.Host != "" && link.Host != strings.TrimPrefix(app.baseURL, "http://") {
		t.Fatalf("link_url host = %q, want the service host", link.Host)
	}

	// The login page must not start the SSO round trip before initData is
	// verified: otherwise the page would be an open redirect to the provider.
	requestsBefore := idp.requestCount()
	resp, body = client.fetch(linkURL)
	requireStatus(t, resp, http.StatusOK, body)
	if !strings.Contains(body, "telegram-web-app.js") {
		t.Error("login page does not load the Telegram Mini App script")
	}
	if got := idp.requestCount(); got != requestsBefore {
		t.Errorf("provider saw %d authorization requests, want %d", got, requestsBefore)
	}

	// Foreign initData is rejected: this is the account substitution attack.
	foreign := initData(t, 424242, time.Now())
	resp, body = client.do(http.MethodPost, app.baseURL+"/auth/telegram/verify",
		fmt.Sprintf(`{"state":%q,"init_data":%q}`, state, foreign), "application/json", "")
	requireStatus(t, resp, http.StatusForbidden, body)
	if payload := decodeJSON[map[string]string](t, body); payload["code"] != "identity_mismatch" {
		t.Errorf("error code = %q, want identity_mismatch; body: %s", payload["code"], body)
	}
	if got := app.bindingCount(t); got != 0 {
		t.Fatalf("bindings after a foreign initData = %d, want 0", got)
	}

	// Even a direct callback is refused while initData has not been verified,
	// so a victim completing the SSO login cannot bind the attacker's account.
	callbackURL := app.authorizeFor(t, idp, state)
	resp, body = client.do(http.MethodGet, callbackURL, "", "", "")
	requireStatus(t, resp, http.StatusForbidden, body)
	if got := app.bindingCount(t); got != 0 {
		t.Fatalf("bindings after an unverified callback = %d, want 0", got)
	}

	// The right account verifies.
	own := initData(t, telegramUserID, time.Now())
	resp, body = client.do(http.MethodPost, app.baseURL+"/auth/telegram/verify",
		fmt.Sprintf(`{"state":%q,"init_data":%q}`, state, own), "application/json", "")
	requireStatus(t, resp, http.StatusOK, body)
	redirectURL := decodeJSON[map[string]string](t, body)["redirect_url"]
	if !strings.HasPrefix(redirectURL, "/auth/login?state=") {
		t.Fatalf("redirect_url = %q, want the login page of the same state", redirectURL)
	}

	// Continue: login page → provider → callback → confirmation page.
	resp, body = client.fetch(redirectURL)
	requireStatus(t, resp, http.StatusFound, body)
	authorizeURL := location(t, resp)
	resp, body = client.do(http.MethodGet, authorizeURL, "", "", "")
	requireStatus(t, resp, http.StatusFound, body)
	confirmCallback := location(t, resp)
	resp, body = client.do(http.MethodGet, confirmCallback, "", "", "")
	requireStatus(t, resp, http.StatusOK, body)
	for _, want := range []string{"Привязка аккаунта", "Telegram", "279058397", "Телеграм Пользователь"} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation page does not contain %q", want)
		}
	}
	if got := app.bindingCount(t); got != 0 {
		t.Fatalf("bindings before the confirmation POST = %d, want 0", got)
	}
	confirmToken := formToken(t, body)

	// A wrong confirmation token is refused and leaves the flow intact.
	resp, body = client.postForm("/auth/confirm", url.Values{"state": {state}, "token": {"not-the-token"}})
	requireStatus(t, resp, http.StatusForbidden, body)
	if got := app.bindingCount(t); got != 0 {
		t.Fatalf("bindings after a wrong confirmation token = %d, want 0", got)
	}

	// The confirmation creates the binding and thanks the user in the chat.
	resp, body = client.postForm("/auth/confirm", url.Values{"state": {state}, "token": {confirmToken}})
	requireStatus(t, resp, http.StatusOK, body)
	if !strings.Contains(body, "Аккаунт привязан") {
		t.Errorf("success page is missing: %s", body)
	}
	if got := app.bindingCount(t); got != 1 {
		t.Fatalf("bindings after confirmation = %d, want 1", got)
	}
	linked := app.linkedBindings()
	if len(linked) != 1 {
		t.Fatalf("OnLinked calls = %d, want 1", len(linked))
	}
	if linked[0].RemoteUserID != "279058397" || linked[0].Platform != "telegram" {
		t.Errorf("linked binding = %+v", linked[0])
	}

	// Replays are impossible: the confirmation token, the state and the
	// verification endpoint all reject a used flow.
	resp, body = client.postForm("/auth/confirm", url.Values{"state": {state}, "token": {confirmToken}})
	requireStatus(t, resp, http.StatusGone, body)
	resp, body = client.fetch(linkURL)
	requireStatus(t, resp, http.StatusGone, body)
	resp, body = client.do(http.MethodPost, app.baseURL+"/auth/telegram/verify",
		fmt.Sprintf(`{"state":%q,"init_data":%q}`, state, own), "application/json", "")
	requireStatus(t, resp, http.StatusGone, body)
	resp, body = client.do(http.MethodGet, confirmCallback, "", "", "")
	requireStatus(t, resp, http.StatusGone, body)
	if got := app.bindingCount(t); got != 1 {
		t.Fatalf("bindings after the replays = %d, want 1", got)
	}

	// With the binding in place the Mini App receives a bearer session.
	resp, body = client.postJSON("/api/v1/auth/telegram", fmt.Sprintf(`{"init_data":%q}`, own))
	requireStatus(t, resp, http.StatusOK, body)
	login := decodeJSON[telegramLoginResponse](t, body)
	if login.Status != "ok" {
		t.Fatalf("status = %q, want ok; body: %s", login.Status, body)
	}
	if login.Token == "" || login.ExpiresAt.IsZero() {
		t.Fatalf("session is incomplete: %+v", login)
	}
	if login.User == nil || len(login.User.Bindings) != 1 {
		t.Fatalf("user payload = %+v, want one binding", login.User)
	}
	if login.User.ID == "" || login.User.Bindings[0].RemoteUserID != "279058397" {
		t.Errorf("user payload = %+v", login.User)
	}

	resp, body = client.getWithBearer("/api/v1/me", login.Token)
	requireStatus(t, resp, http.StatusOK, body)
	if me := decodeJSON[meResponse](t, body); len(me.Bindings) != 1 {
		t.Fatalf("GET /me bindings = %+v, want one", me.Bindings)
	}
}

// TestTelegramInitDataRejections covers the negative cases of M1-5 that do not
// need a pending flow.
func TestTelegramInitDataRejections(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	client := app.newClient()

	tests := []struct {
		name     string
		initData string
	}{
		{
			name:     "forged signature",
			initData: initDataWithToken(t, "111111:wrong-token", 279058397, time.Now()),
		},
		{
			name:     "expired auth_date",
			initData: initData(t, 279058397, time.Now().Add(-25*time.Hour)),
		},
		{
			name:     "garbage",
			initData: "not-a-query-string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := client.postJSON("/api/v1/auth/telegram",
				fmt.Sprintf(`{"init_data":%q}`, tt.initData))
			requireStatus(t, resp, http.StatusUnauthorized, body)
			if payload := decodeJSON[map[string]string](t, body); payload["code"] != "invalid_init_data" {
				t.Errorf("error code = %q, want invalid_init_data", payload["code"])
			}
		})
	}

	// A valid payload of an unbound account is a normal link_required answer,
	// not an error.
	resp, body := client.postJSON("/api/v1/auth/telegram",
		fmt.Sprintf(`{"init_data":%q}`, initData(t, 555000111, time.Now())))
	requireStatus(t, resp, http.StatusOK, body)
	if got := decodeJSON[telegramLoginResponse](t, body).Status; got != "link_required" {
		t.Fatalf("status = %q, want link_required", got)
	}

	// A body without init_data is a bad request.
	resp, body = client.postJSON("/api/v1/auth/telegram", `{}`)
	requireStatus(t, resp, http.StatusBadRequest, body)
}

// authorizeFor asks the fake provider for a code for an existing state. It is
// how the test simulates a victim completing the SSO login for somebody else's
// pending link.
func (a *testApp) authorizeFor(t *testing.T, idp *fakeIDP, state string) string {
	t.Helper()
	verifier := "attacker-verifier"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", testClientID)
	query.Set("redirect_uri", a.baseURL+"/auth/callback")
	query.Set("state", state)
	query.Set("nonce", "attacker-nonce")
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")

	client := a.newClient()
	resp, body := client.do(http.MethodGet, idp.issuer+"/authorize?"+query.Encode(), "", "", "")
	requireStatus(t, resp, http.StatusFound, body)
	return location(t, resp)
}

// bindingCount reports how many messenger bindings exist.
func (a *testApp) bindingCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := a.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM messenger_bindings`).Scan(&count); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	return count
}
