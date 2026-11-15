package postgres_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	interactionpg "github.com/maksimkurb/cublyverse/internal/interaction/postgres"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// uuidString renders a uuid column in the canonical 8-4-4-4-12 form.
func uuidString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TestInteractionStore is the M1-2 acceptance test: an action survives a
// process restart (a new pool over the same database) and an expired action is
// not resolved. It also covers the M2-3 additions: the principal restriction
// and the opaque payload.
func TestInteractionStore(t *testing.T) {
	databaseURL := testdb.Start(t)
	ctx := context.Background()
	logger := testLogger()
	if err := store.Migrate(ctx, databaseURL, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	first, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	storeOne := interactionpg.New(first)

	user, err := first.Queries().CreateUser(ctx, db.CreateUserParams{
		OidcIssuer:  "https://id.example.com",
		OidcSubject: "subject-1",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	principal := unibot.PrincipalID(uuidString(user.ID))

	action := unibot.Interaction{
		ActionID:       "reminders.done",
		ExpectedUserID: "279058397",
		PrincipalID:    principal,
		Payload:        "reminder-7:occurrence-9",
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	if err := storeOne.PutToken(ctx, "token-survives-restart", action); err != nil {
		t.Fatalf("PutToken: %v", err)
	}
	expired := action
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	if err := storeOne.PutToken(ctx, "token-expired", expired); err != nil {
		t.Fatalf("PutToken(expired): %v", err)
	}
	// An interaction without an explicit expiry gets the 24 hour default.
	noExpiry := action
	noExpiry.ExpiresAt = time.Time{}
	noExpiry.PrincipalID = ""
	noExpiry.Payload = ""
	if err := storeOne.PutToken(ctx, "token-default-ttl", noExpiry); err != nil {
		t.Fatalf("PutToken(default ttl): %v", err)
	}
	if err := storeOne.PutMessageKey(ctx, unibot.PlatformMatrix, "!room:example.com", "$event", "👍", unibot.Interaction{
		ChoiceID:       "environment",
		OptionID:       "prod",
		ExpectedUserID: "@user:example.com",
		PrincipalID:    principal,
		ExpiresAt:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutMessageKey: %v", err)
	}

	// Restart: close the pool and open a new one, exactly like a redeploy.
	first.Close()
	second, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(second.Close)
	storeTwo := interactionpg.New(second)

	got, ok, err := storeTwo.ResolveToken(ctx, "token-survives-restart")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if !ok {
		t.Fatal("the interaction did not survive the restart")
	}
	if got.ActionID != action.ActionID || got.ExpectedUserID != action.ExpectedUserID {
		t.Fatalf("resolved interaction = %+v, want %+v", got, action)
	}
	if got.PrincipalID != principal {
		t.Errorf("principal_id = %q, want %q", got.PrincipalID, principal)
	}
	if got.Payload != action.Payload {
		t.Errorf("payload = %q, want %q", got.Payload, action.Payload)
	}
	if got.ExpiresAt.Unix() != action.ExpiresAt.Unix() {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, action.ExpiresAt)
	}

	// An interaction without a principal keeps the restriction empty.
	defaultTTL, ok, err := storeTwo.ResolveToken(ctx, "token-default-ttl")
	if err != nil {
		t.Fatalf("ResolveToken(default ttl): %v", err)
	}
	if !ok {
		t.Fatal("the interaction with the default ttl was not stored")
	}
	if defaultTTL.PrincipalID != "" {
		t.Errorf("principal_id = %q, want empty", defaultTTL.PrincipalID)
	}
	if ttl := time.Until(defaultTTL.ExpiresAt); ttl < unibot.DefaultActionTTL-time.Minute || ttl > unibot.DefaultActionTTL {
		t.Errorf("default ttl = %v, want about %v", ttl, unibot.DefaultActionTTL)
	}

	// The stored token is a hash, never the token itself.
	var tokenHash []byte
	if err := second.Pool().QueryRow(ctx, `SELECT token_hash FROM interactions WHERE action_id = $1`, action.ActionID).Scan(&tokenHash); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}
	if string(tokenHash) == "token-survives-restart" {
		t.Fatal("the callback token is stored verbatim")
	}
	if len(tokenHash) != 32 {
		t.Fatalf("token_hash length = %d, want a sha256 digest", len(tokenHash))
	}

	// Expired and unknown tokens are not resolved.
	for _, token := range []string{"token-expired", "unknown-token"} {
		if _, ok, err := storeTwo.ResolveToken(ctx, token); err != nil {
			t.Fatalf("ResolveToken(%q): %v", token, err)
		} else if ok {
			t.Errorf("ResolveToken(%q) = true, want false", token)
		}
	}

	// Message keys work the same way.
	key, ok, err := storeTwo.ResolveMessageKey(ctx, unibot.PlatformMatrix, "!room:example.com", "$event", "👍")
	if err != nil {
		t.Fatalf("ResolveMessageKey: %v", err)
	}
	if !ok || key.ChoiceID != "environment" || key.OptionID != "prod" {
		t.Fatalf("resolved message key = (%+v, %v)", key, ok)
	}
	if key.PrincipalID != principal {
		t.Errorf("message key principal = %q, want %q", key.PrincipalID, principal)
	}
	// Re-registering the same button replaces the target instead of failing.
	if err := storeTwo.PutMessageKey(ctx, unibot.PlatformMatrix, "!room:example.com", "$event", "👍", unibot.Interaction{
		ChoiceID:  "environment",
		OptionID:  "dev",
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutMessageKey upsert: %v", err)
	}
	key, _, err = storeTwo.ResolveMessageKey(ctx, unibot.PlatformMatrix, "!room:example.com", "$event", "👍")
	if err != nil {
		t.Fatalf("ResolveMessageKey after upsert: %v", err)
	}
	if key.OptionID != "dev" {
		t.Fatalf("option after upsert = %q, want dev", key.OptionID)
	}
	if key.PrincipalID != "" {
		t.Errorf("principal after upsert = %q, want empty", key.PrincipalID)
	}
	if _, ok, err := storeTwo.ResolveMessageKey(ctx, unibot.PlatformMatrix, "!room:example.com", "$other", "👍"); err != nil || ok {
		t.Fatalf("ResolveMessageKey for another message = (%v, %v), want false", ok, err)
	}

	// Cleanup drops expired rows and keeps live ones.
	deleted, err := storeTwo.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Cleanup deleted %d rows, want 1", deleted)
	}
	if _, ok, err := storeTwo.ResolveToken(ctx, "token-survives-restart"); err != nil || !ok {
		t.Fatalf("live interaction disappeared after cleanup: ok=%v err=%v", ok, err)
	}
}

// TestPrincipalRestrictionRequiresAUser checks that the principal restriction
// references a real user: an arbitrary principal cannot be stored.
func TestPrincipalRestrictionRequiresAUser(t *testing.T) {
	databaseURL := testdb.Start(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL, testLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	interactions := interactionpg.New(st)
	err = interactions.PutToken(ctx, "token", unibot.Interaction{
		ActionID:    "reminders.done",
		PrincipalID: "00000000-0000-0000-0000-000000000001",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("PutToken with an unknown principal succeeded, want a foreign key violation")
	}
	// An empty principal is "no restriction" and must be accepted.
	if err := interactions.PutToken(ctx, "token", unibot.Interaction{
		ActionID:  "reminders.done",
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutToken without a principal: %v", err)
	}
}
