package catalog_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

// fakeSource is one deterministic fetch. Real network parsing is covered by
// the parser tests; the syncer only needs entries.
type fakeSource struct {
	name    string
	entries []catalog.Entry
	err     error
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Fetch(context.Context) ([]catalog.Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]catalog.Entry(nil), f.entries...), nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	logger := quietLogger()
	url := testdb.Start(t)
	if err := store.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func bead(article, name, color string) catalog.Entry {
	return catalog.Entry{
		Category:     catalog.CategorySeedBeads,
		Manufacturer: catalog.ManufacturerPreciosa,
		Article:      article,
		Series:       "311-19001",
		Name:         name,
		ColorName:    color,
		Size:         "10/0",
		Unit:         catalog.UnitPack,
		SourceURL:    "https://example.test/detail/" + article,
		ImageURL:     "https://example.test/" + article + ".webp",
		ExternalID:   article,
	}
}

func itemByArticle(t *testing.T, st *store.Store, article string) db.CraftCatalogItem {
	t.Helper()
	item, err := st.Queries().GetGlobalCraftCatalogItemByArticle(context.Background(), mustIdentity(t, st, article))
	if err != nil {
		t.Fatalf("get %q: %v", article, err)
	}
	return item
}

// mustIdentity resolves the identity a synced row is stored under.
func mustIdentity(t *testing.T, st *store.Store, article string) db.GetGlobalCraftCatalogItemByArticleParams {
	t.Helper()
	ctx := context.Background()
	q := st.Queries()
	category, err := q.GetReferenceCraftCategoryBySlug(ctx, catalog.CategorySeedBeads)
	if err != nil {
		t.Fatalf("get category: %v", err)
	}
	manufacturer, err := q.UpsertCraftManufacturer(ctx, db.UpsertCraftManufacturerParams{Name: catalog.ManufacturerPreciosa})
	if err != nil {
		t.Fatalf("get manufacturer: %v", err)
	}
	return db.GetGlobalCraftCatalogItemByArticleParams{
		ManufacturerID: manufacturer.ID,
		CategoryID:     category.ID,
		Article:        article,
	}
}

func TestSyncerCreatesThenIsIdempotent(t *testing.T) {
	st := newStore(t)
	syncer := catalog.NewSyncer(st, quietLogger())
	source := &fakeSource{name: "preciosa", entries: []catalog.Entry{
		bead("311-19001-10_0-02090", "Rocailles 02090", "alabaster white"),
		bead("311-19001-10_0-20010", "Rocailles 20010", "lt. amethyst"),
		bead("311-19001-10_0-20060", "Rocailles 20060", "amethyst"),
	}}

	first, err := syncer.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if first.Created != 3 || first.Updated != 0 || first.Unchanged != 0 {
		t.Fatalf("first Run() = %+v, want 3 created", first)
	}
	if first.RunID == 0 {
		t.Fatal("first Run() did not record a run id")
	}

	second, err := syncer.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Created != 0 || second.Updated != 0 || second.Unchanged != 3 {
		t.Fatalf("second Run() = %+v, want everything unchanged", second)
	}

	item, err := st.Queries().GetGlobalCraftCatalogItemByArticle(context.Background(), mustIdentity(t, st, "311-19001-10_0-02090"))
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if item.WorkspaceID.Valid {
		t.Fatal("synced item has a workspace: it must be global")
	}
	if item.Source != "preciosa" || item.ColorName != "alabaster white" {
		t.Fatalf("item source/colour = %q/%q", item.Source, item.ColorName)
	}
	if !item.FetchedAt.Valid || !item.DeprecatedAt.Time.IsZero() {
		t.Fatalf("fetched_at/deprecated_at = %+v/%+v", item.FetchedAt, item.DeprecatedAt)
	}

	var status string
	var created, updated, unchanged int
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT status, created, updated, unchanged FROM craft_catalog_sync_runs WHERE id = $1`,
		second.RunID,
	).Scan(&status, &created, &updated, &unchanged); err != nil {
		t.Fatalf("read run row: %v", err)
	}
	if status != "ok" || created != 0 || updated != 0 || unchanged != 3 {
		t.Fatalf("run row = %s/%d/%d/%d", status, created, updated, unchanged)
	}
}

func TestSyncerRespectsManualOverride(t *testing.T) {
	st := newStore(t)
	syncer := catalog.NewSyncer(st, quietLogger())
	source := &fakeSource{name: "preciosa", entries: []catalog.Entry{
		bead("311-19001-10_0-02090", "Official name", "alabaster white"),
	}}

	if _, err := syncer.Run(context.Background(), source); err != nil {
		t.Fatalf("seed Run() error = %v", err)
	}
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE craft_catalog_items SET name = 'Мой бисер', manual_override = '{"name": true}'::jsonb WHERE article = $1`,
		"311-19001-10_0-02090",
	); err != nil {
		t.Fatalf("pin name: %v", err)
	}

	source.entries = []catalog.Entry{
		bead("311-19001-10_0-02090", "Official name v2", "blue"),
	}
	result, err := syncer.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Updated != 1 || result.Created != 0 || result.Unchanged != 0 {
		t.Fatalf("Run() = %+v, want one updated row", result)
	}

	item, err := st.Queries().GetGlobalCraftCatalogItemByArticle(context.Background(), mustIdentity(t, st, "311-19001-10_0-02090"))
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if item.Name != "Мой бисер" {
		t.Fatalf("name = %q, the manual override was overwritten", item.Name)
	}
	if item.ColorName != "blue" {
		t.Fatalf("colour name = %q, want the unpinned field updated", item.ColorName)
	}

	// A fetch that differs only in the pinned field is a no-op.
	source.entries = []catalog.Entry{
		bead("311-19001-10_0-02090", "Official name v3", "blue"),
	}
	result, err = syncer.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Updated != 0 || result.Unchanged != 1 {
		t.Fatalf("Run() = %+v, want the pinned field ignored", result)
	}
}

func TestSyncerPartialFetchDoesNotDeprecate(t *testing.T) {
	st := newStore(t)
	syncer := catalog.NewSyncer(st, quietLogger())
	full := []catalog.Entry{
		bead("A1", "Bead A1", "red"),
		bead("A2", "Bead A2", "green"),
		bead("A3", "Bead A3", "blue"),
		bead("A4", "Bead A4", "black"),
	}
	source := &fakeSource{name: "preciosa", entries: full}
	if _, err := syncer.Run(context.Background(), source); err != nil {
		t.Fatalf("seed Run() error = %v", err)
	}

	// A quarter of the previous catalogue is a partial download: nothing may be
	// deprecated.
	source.entries = full[:1]
	result, err := syncer.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("partial Run() error = %v", err)
	}
	if result.Deprecated != 0 {
		t.Fatalf("partial Run() deprecated %d items, want 0", result.Deprecated)
	}
	if active := activeItems(t, st); active != 4 {
		t.Fatalf("active items after partial fetch = %d, want 4", active)
	}

	// Three quarters is a believable catalogue with one removal.
	source.entries = full[:3]
	result, err = syncer.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("shrunk Run() error = %v", err)
	}
	if result.Deprecated != 1 {
		t.Fatalf("shrunk Run() deprecated %d items, want 1", result.Deprecated)
	}
	if active := activeItems(t, st); active != 3 {
		t.Fatalf("active items = %d, want 3", active)
	}
	if item := itemByArticle(t, st, "A4"); item.DeprecatedAt.Time.IsZero() {
		t.Fatal("A4 is still active after a full-ish fetch removed it")
	}

	// An item returning to the catalogue is un-deprecated without a manual
	// intervention.
	source.entries = full
	if _, err := syncer.Run(context.Background(), source); err != nil {
		t.Fatalf("restored Run() error = %v", err)
	}
	if item := itemByArticle(t, st, "A4"); !item.DeprecatedAt.Time.IsZero() {
		t.Fatalf("A4 stayed deprecated: %+v", item.DeprecatedAt)
	}
	if active := activeItems(t, st); active != 4 {
		t.Fatalf("active items after restore = %d, want 4", active)
	}
}

func activeItems(t *testing.T, st *store.Store) int {
	t.Helper()
	var count int
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM craft_catalog_items WHERE deprecated_at IS NULL AND workspace_id IS NULL`,
	).Scan(&count); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return count
}
