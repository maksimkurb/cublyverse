package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// TestConnectionsAPI lists and revokes OAuth authorizations over HTTP and keeps
// them private to their owner.
func TestConnectionsAPI(t *testing.T) {
	env := newMCPTestApp(t)
	ctx := context.Background()
	q := env.app.Store().Queries()

	if _, err := q.UpsertOAuthClient(ctx, db.UpsertOAuthClientParams{
		ClientID: "client-1", Kind: "dcr", Name: "Claude", RedirectUris: []string{"https://claude.example/cb"},
	}); err != nil {
		t.Fatal(err)
	}
	owner, _ := workspace.ParseID(env.user.ID)
	family, _ := workspace.ParseID("11111111-1111-4111-8111-111111111111")
	if _, err := q.CreateOAuthToken(ctx, db.CreateOAuthTokenParams{
		FamilyID: family, ClientID: "client-1", UserID: owner, Scopes: []string{"profile:read"},
		AccessHash: []byte("a"), AccessExpiresAt: farFuture(), RefreshHash: []byte("r"), RefreshExpiresAt: farFuture(),
	}); err != nil {
		t.Fatal(err)
	}

	status, body := env.api(http.MethodGet, "/connections", env.session.Token, "")
	if status != http.StatusOK {
		t.Fatalf("list status %d: %s", status, body)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list) != 1 || list[0]["client_name"] != "Claude" {
		t.Fatalf("list = %s (%v)", body, err)
	}
	id := list[0]["id"].(string)

	// Another user neither sees nor can revoke it.
	other := env.createUser("someone-else")
	otherSession, err := env.app.Auth().CreateSession(ctx, other.ID, "bearer")
	if err != nil {
		t.Fatal(err)
	}
	if status, _ = env.api(http.MethodDelete, "/connections/"+id, otherSession.Token, ""); status != http.StatusNotFound {
		t.Fatalf("foreign revoke status = %d, want 404", status)
	}

	// A personal access token may not manage connections.
	if status, _ = env.api(http.MethodGet, "/connections", env.issueToken("profile:read"), ""); status != http.StatusForbidden {
		t.Fatalf("PAT list status = %d, want 403", status)
	}

	if status, body = env.api(http.MethodDelete, "/connections/"+id, env.session.Token, ""); status != http.StatusNoContent {
		t.Fatalf("revoke status %d: %s", status, body)
	}
	_, body = env.api(http.MethodGet, "/connections", env.session.Token, "")
	if body != "[]\n" && body != "[]" {
		t.Fatalf("list after revoke = %q, want empty", body)
	}
}

func farFuture() time.Time { return time.Now().Add(24 * time.Hour) }
