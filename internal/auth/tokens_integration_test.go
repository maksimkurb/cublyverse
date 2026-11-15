package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/auth"
)

// patPattern is the documented format: the marker and 43 URL-safe base64
// characters of 32 random bytes.
var patPattern = regexp.MustCompile(`^cv_pat_[A-Za-z0-9_-]{43}$`)

// tokenList decodes GET /api/v1/tokens.
type tokenList []struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string  `json:"expires_at"`
	LastUsedAt *string  `json:"last_used_at"`
	RevokedAt  *string  `json:"revoked_at"`
	CreatedAt  string   `json:"created_at"`
}

// createdToken decodes POST /api/v1/tokens.
type createdToken struct {
	Token    string `json:"token"`
	APIToken struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Prefix string   `json:"prefix"`
		Scopes []string `json:"scopes"`
	} `json:"api_token"`
}

func (a *testApp) createTokenHTTP(t *testing.T, session auth.Session, body string) (createdToken, string, int) {
	t.Helper()
	client := a.newClient()
	resp, payload := client.do(http.MethodPost, a.baseURL+"/api/v1/tokens", body, "application/json", session.Token)
	return decodeJSON[createdToken](t, payload), payload, resp.StatusCode
}

// TestAPITokens covers the M5-1 acceptance surface: creation, the one-time
// plaintext, storage of a hash only, listing, revocation, scope limits and the
// rule that a token can never manage tokens.
func TestAPITokens(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, nil)
	owner := app.seedUser(t, "pat-owner")
	other := app.seedUser(t, "pat-other")
	session := app.seedSession(t, owner.ID, auth.SessionKindBearer)

	t.Run("create returns the plaintext once and stores a hash", func(t *testing.T) {
		created, payload, status := app.createTokenHTTP(t, session,
			`{"name":"Claude Code","scopes":["reminders:read","reminders:write"],"expires_in_days":30}`)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", status, payload)
		}
		if !patPattern.MatchString(created.Token) {
			t.Fatalf("token %q does not match %s", created.Token, patPattern)
		}
		if created.APIToken.Name != "Claude Code" {
			t.Errorf("name = %q, want %q", created.APIToken.Name, "Claude Code")
		}
		if created.APIToken.Prefix != created.Token[len(auth.APITokenPrefix):len(auth.APITokenPrefix)+8] {
			t.Errorf("prefix = %q, want the 8 characters after the marker", created.APIToken.Prefix)
		}
		if len(created.APIToken.Scopes) != 2 {
			t.Errorf("scopes = %v, want two", created.APIToken.Scopes)
		}

		// The database keeps sha256 only: the plaintext must not appear.
		var storedHash []byte
		var storedPrefix string
		if err := app.store.Pool().QueryRow(context.Background(),
			"SELECT token_hash, token_prefix FROM api_tokens WHERE id = $1", created.APIToken.ID,
		).Scan(&storedHash, &storedPrefix); err != nil {
			t.Fatalf("read stored token: %v", err)
		}
		if len(storedHash) != 32 {
			t.Errorf("stored hash is %d bytes, want 32 (sha256)", len(storedHash))
		}
		if strings.Contains(string(storedHash), created.Token) {
			t.Error("stored hash contains the plaintext")
		}
		if storedPrefix != created.APIToken.Prefix {
			t.Errorf("stored prefix = %q, want %q", storedPrefix, created.APIToken.Prefix)
		}

		// The list never carries the secret.
		client := app.newClient()
		resp, listBody := client.do(http.MethodGet, app.baseURL+"/api/v1/tokens", "", "", session.Token)
		requireStatus(t, resp, http.StatusOK, listBody)
		items := decodeJSON[tokenList](t, listBody)
		if len(items) != 1 {
			t.Fatalf("list has %d items, want 1; body: %s", len(items), listBody)
		}
		if strings.Contains(listBody, created.Token) {
			t.Error("the token list leaks the plaintext")
		}
	})

	t.Run("token authenticates API requests and records last use", func(t *testing.T) {
		created, _, status := app.createTokenHTTP(t, session,
			`{"name":"api-reader","scopes":["reminders:read"]}`)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201", status)
		}

		client := app.newClient()
		resp, body := client.getWithBearer("/api/v1/me", created.Token)
		requireStatus(t, resp, http.StatusOK, body)
		profile := decodeJSON[meResponse](t, body)
		if profile.ID != owner.ID {
			t.Errorf("GET /me with a token returned %q, want %q", profile.ID, owner.ID)
		}

		// last_used_at is written on use (at most once per minute, so the first
		// request is enough to see it).
		var lastUsed *time.Time
		if err := app.store.Pool().QueryRow(context.Background(),
			"SELECT last_used_at FROM api_tokens WHERE id = $1", created.APIToken.ID,
		).Scan(&lastUsed); err != nil {
			t.Fatalf("read last_used_at: %v", err)
		}
		if lastUsed == nil {
			t.Error("last_used_at was not recorded on the first use")
		}
	})

	t.Run("unknown, revoked and expired tokens answer 401", func(t *testing.T) {
		client := app.newClient()

		resp, body := client.getWithBearer("/api/v1/me", "cv_pat_"+strings.Repeat("A", 43))
		requireStatus(t, resp, http.StatusUnauthorized, body)
		if payload := decodeErrorPayload(t, body); payload.Code != "unauthorized" {
			t.Errorf("code = %q, want unauthorized", payload.Code)
		}

		created, _, status := app.createTokenHTTP(t, session, `{"name":"to-revoke","scopes":["reminders:read"]}`)
		if status != http.StatusCreated {
			t.Fatalf("create: status = %d, want 201", status)
		}
		resp, body = client.getWithBearer("/api/v1/me", created.Token)
		requireStatus(t, resp, http.StatusOK, body)

		revoke := app.newClient()
		resp, body = revoke.do(http.MethodDelete, app.baseURL+"/api/v1/tokens/"+created.APIToken.ID,
			"", "application/json", session.Token)
		requireStatus(t, resp, http.StatusNoContent, body)

		resp, body = client.getWithBearer("/api/v1/me", created.Token)
		requireStatus(t, resp, http.StatusUnauthorized, body)

		expired, _, status := app.createTokenHTTP(t, session, `{"name":"to-expire","scopes":["reminders:read"]}`)
		if status != http.StatusCreated {
			t.Fatalf("create: status = %d, want 201", status)
		}
		if _, err := app.store.Pool().Exec(context.Background(),
			"UPDATE api_tokens SET expires_at = now() - interval '1 minute' WHERE id = $1",
			expired.APIToken.ID,
		); err != nil {
			t.Fatalf("expire token: %v", err)
		}
		resp, body = client.getWithBearer("/api/v1/me", expired.Token)
		requireStatus(t, resp, http.StatusUnauthorized, body)
	})

	t.Run("a token can neither manage tokens nor change the profile", func(t *testing.T) {
		created, _, status := app.createTokenHTTP(t, session,
			`{"name":"claude","scopes":["profile:read","reminders:read","reminders:write"]}`)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201", status)
		}
		client := app.newClient()

		// Listing, creating and revoking tokens is session-only.
		resp, body := client.getWithBearer("/api/v1/tokens", created.Token)
		requireStatus(t, resp, http.StatusForbidden, body)
		if payload := decodeErrorPayload(t, body); payload.Code != "forbidden" {
			t.Errorf("GET /tokens code = %q, want forbidden", payload.Code)
		}

		resp, body = client.do(http.MethodPost, app.baseURL+"/api/v1/tokens",
			`{"name":"escalation","scopes":["reminders:write"]}`, "application/json", created.Token)
		requireStatus(t, resp, http.StatusForbidden, body)

		resp, body = client.do(http.MethodDelete, app.baseURL+"/api/v1/tokens/"+created.APIToken.ID,
			"", "application/json", created.Token)
		requireStatus(t, resp, http.StatusForbidden, body)

		// The token must still be alive: a forbidden call is not a revocation.
		resp, body = client.getWithBearer("/api/v1/me", created.Token)
		requireStatus(t, resp, http.StatusOK, body)

		// Profile mutations need a session.
		resp, body = client.patchJSON("/api/v1/me", `{"timezone":"Europe/Moscow"}`, created.Token)
		requireStatus(t, resp, http.StatusForbidden, body)
	})

	t.Run("ownership isolates tokens", func(t *testing.T) {
		created, _, status := app.createTokenHTTP(t, session, `{"name":"owned","scopes":["reminders:read"]}`)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201", status)
		}

		otherSession := app.seedSession(t, other.ID, auth.SessionKindBearer)
		otherClient := app.newClient()

		resp, body := otherClient.do(http.MethodGet, app.baseURL+"/api/v1/tokens", "", "", otherSession.Token)
		requireStatus(t, resp, http.StatusOK, body)
		if strings.Contains(body, created.APIToken.ID) {
			t.Error("another user's token list contains the token")
		}

		resp, body = otherClient.do(http.MethodDelete, app.baseURL+"/api/v1/tokens/"+created.APIToken.ID,
			"", "application/json", otherSession.Token)
		requireStatus(t, resp, http.StatusNotFound, body)

		var revoked *time.Time
		if err := app.store.Pool().QueryRow(context.Background(),
			"SELECT revoked_at FROM api_tokens WHERE id = $1", created.APIToken.ID,
		).Scan(&revoked); err != nil {
			t.Fatalf("read revoked_at: %v", err)
		}
		if revoked != nil {
			t.Error("another user revoked the token")
		}
	})

	t.Run("validation rejects bad input", func(t *testing.T) {
		cases := []struct {
			name string
			body string
		}{
			{"empty name", `{"name":"  ","scopes":["reminders:read"]}`},
			{"unknown scope", `{"name":"x","scopes":["reminders:admin"]}`},
			{"no scopes", `{"name":"x","scopes":[]}`},
			{"expiry too long", `{"name":"x","scopes":["reminders:read"],"expires_in_days":366}`},
			{"expiry zero", `{"name":"x","scopes":["reminders:read"],"expires_in_days":0}`},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				_, payload, status := app.createTokenHTTP(t, session, test.body)
				if status != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body: %s", status, payload)
				}
				if decodeErrorPayload(t, payload).Code != "bad_request" {
					t.Errorf("code is not bad_request: %s", payload)
				}
			})
		}
	})
}

// decodeErrorPayload decodes the shared error shape.
func decodeErrorPayload(t *testing.T, body string) struct {
	Code    string `json:"code"`
	Message string `json:"message"`
} {
	t.Helper()
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode error payload %q: %v", body, err)
	}
	return payload
}
