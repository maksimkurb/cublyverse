package catalog_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

// TestSyncDue proves a restart does not start a new crawl while the stored runs
// show a fresh or very recent one.
func TestSyncDue(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	url := testdb.Start(t)
	if err := store.Migrate(ctx, url, logger); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	syncer := catalog.NewSyncer(st, logger)
	const week = 7 * 24 * time.Hour

	check := func(name string, after time.Duration, want bool) {
		t.Helper()
		due, err := syncer.Due(ctx, "dmc", week, time.Now().Add(after))
		if err != nil || due != want {
			t.Fatalf("%s: due = %v, want %v (%v)", name, due, want, err)
		}
	}
	insert := func(status string) {
		t.Helper()
		if _, err := st.Pool().Exec(ctx, "DELETE FROM craft_catalog_sync_runs"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Pool().Exec(ctx, "INSERT INTO craft_catalog_sync_runs (source, status) VALUES ('dmc', $1)", status); err != nil {
			t.Fatal(err)
		}
	}

	check("never synced", 0, true)
	insert("ok")
	check("fresh success", time.Hour, false)
	check("stale success", week+time.Hour, true)
	insert("running")
	check("just interrupted", time.Minute, false)
	check("interrupted long ago", 3*time.Hour, true)
	insert("failed")
	check("just failed", time.Hour, false)
	check("failed yesterday", 25*time.Hour, true)
}
