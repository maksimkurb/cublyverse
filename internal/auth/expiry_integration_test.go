package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/auth"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// TestExpiredCredentials proves that expiry is enforced by PostgreSQL itself:
// nothing in the application has to remember to check it.
func TestExpiredCredentials(t *testing.T) {
	idp := newFakeIDP(t)
	app := newTestApp(t, idp, func(cfg *auth.Config) {
		cfg.PendingTTL = time.Nanosecond
		cfg.BearerSessionTTL = time.Nanosecond
		cfg.CookieSessionTTL = time.Nanosecond
	})
	user := app.seedUser(t, "expired")
	client := app.newClient()
	ctx := context.Background()

	// An expired session is rejected even though the token hash matches.
	session := app.seedSession(t, user.ID, auth.SessionKindBearer)
	resp, body := client.getWithBearer("/api/v1/me", session.Token)
	requireStatus(t, resp, http.StatusUnauthorized, body)

	// An expired login link is dead on arrival.
	linkURL, err := app.auth.BeginMessengerBinding(ctx, unibot.PlatformTelegram, "123456", "123456")
	if err != nil {
		t.Fatalf("BeginMessengerBinding: %v", err)
	}
	resp, body = client.do(http.MethodGet, linkURL, "", "", "")
	requireStatus(t, resp, http.StatusGone, body)

	// Its state cannot be used by the Mini App either.
	parsed, err := url.Parse(linkURL)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	state := parsed.Query().Get("state")
	resp, body = client.do(http.MethodPost, app.baseURL+"/auth/telegram/verify",
		fmt.Sprintf(`{"state":%q,"init_data":%q}`, state, initData(t, 123456, time.Now())),
		"application/json", "")
	requireStatus(t, resp, http.StatusGone, body)
}
