package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	interactionpg "github.com/maksimkurb/cublyverse/internal/interaction/postgres"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestInteractionStore is the M1-2 acceptance test: an action survives a
// process restart (a new pool over the same database) and an expired action is
// not resolved.
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

	action := unibot.Interaction{
		ActionID:       "reminders.done:7:9",
		ExpectedUserID: "279058397",
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
	if err := storeOne.PutMessageKey(ctx, unibot.PlatformMatrix, "!room:example.com", "$event", "👍", unibot.Interaction{
		ChoiceID:       "environment",
		OptionID:       "prod",
		ExpectedUserID: "@user:example.com",
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
	if got.ExpiresAt.Unix() != action.ExpiresAt.Unix() {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, action.ExpiresAt)
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
