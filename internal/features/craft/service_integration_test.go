package craft_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

type env struct {
	st         *store.Store
	workspaces *workspace.Service
	craft      *craft.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	url := testdb.Start(t)
	if err := store.Migrate(ctx, url, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	ws := workspace.New(st)
	return &env{st: st, workspaces: ws, craft: craft.New(st, ws)}
}

func (e *env) user(t *testing.T, subject, email string) string {
	t.Helper()
	name := subject
	u, err := e.st.Queries().CreateUser(context.Background(), db.CreateUserParams{
		OidcIssuer: "https://idp", OidcSubject: subject, Email: &email, DisplayName: &name,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return workspace.FormatID(u.ID)
}

// globalItem inserts a global catalogue item the way a scraper would.
func (e *env) globalItem(t *testing.T, article string) string {
	t.Helper()
	ctx := context.Background()
	q := e.st.Queries()
	cat, err := q.GetReferenceCraftCategoryBySlug(ctx, "seed-beads")
	if err != nil {
		t.Fatal(err)
	}
	m, err := q.UpsertCraftManufacturer(ctx, db.UpsertCraftManufacturerParams{Name: "PRECIOSA Ornela"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := q.CreateCraftCatalogItem(ctx, db.CreateCraftCatalogItemParams{
		CategoryID: cat.ID, ManufacturerID: m.ID, Article: article, Name: "PRECIOSA " + article,
		Unit: "упаковка", Attrs: []byte("{}"), Source: "preciosa",
	})
	if err != nil {
		t.Fatal(err)
	}
	return workspace.FormatID(item.ID)
}

func TestWorkspaceMembershipIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	lilya := e.user(t, "lilya", "lilya@example.com")
	maxim := e.user(t, "maxim", "maxim@example.com")
	stranger := e.user(t, "stranger", "x@example.com")

	home, err := e.workspaces.Create(ctx, lilya, workspace.AppWarehouse, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.workspaces.Require(ctx, stranger, home.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("stranger Require = %v, want ErrNotFound", err)
	}

	cands, err := e.workspaces.Candidates(ctx, lilya, home.ID, "")
	if err != nil || len(cands) != 2 {
		t.Fatalf("Candidates = %v, %v; want maxim and stranger", cands, err)
	}
	if err := e.workspaces.AddMember(ctx, lilya, home.ID, maxim); err != nil {
		t.Fatal(err)
	}
	if err := e.workspaces.AddMember(ctx, lilya, home.ID, maxim); err != nil {
		t.Fatalf("second add must be idempotent: %v", err)
	}
	if _, err := e.workspaces.Require(ctx, maxim, home.ID); err != nil {
		t.Fatalf("member Require: %v", err)
	}
	members, _ := e.workspaces.Members(ctx, maxim, home.ID)
	if len(members) != 2 || members[0].AvatarURL == "" {
		t.Fatalf("members = %+v", members)
	}

	// The data of a workspace is invisible to non-members.
	if _, err := e.craft.ListInventory(ctx, stranger, home.ID, craft.CatalogFilter{}); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("stranger ListInventory = %v, want ErrNotFound", err)
	}

	if err := e.workspaces.RemoveMember(ctx, maxim, home.ID, lilya); err != nil {
		t.Fatal(err)
	}
	if err := e.workspaces.RemoveMember(ctx, maxim, home.ID, maxim); !errors.Is(err, workspace.ErrLastMember) {
		t.Fatalf("remove last = %v, want ErrLastMember", err)
	}
}

func TestInventoryAdjustAndLink(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	other := e.user(t, "other", "o@example.com")
	ws, _ := e.workspaces.Create(ctx, user, workspace.AppWarehouse, "Дом")
	otherWS, _ := e.workspaces.Create(ctx, other, workspace.AppWarehouse, "Чужой")

	// A private item for something not in any catalogue.
	pearl, err := e.craft.CreateItem(ctx, user, ws.ID, craft.ItemInput{
		Category: "pearls", Name: "Жемчуг Майорка айвори 2 мм", ColorName: "айвори", Unit: "см",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pearl.Private() || pearl.Unit != "см" {
		t.Fatalf("pearl = %+v", pearl)
	}

	entry, err := e.craft.Adjust(ctx, user, ws.ID, pearl.ID, 32, "из Grist")
	if err != nil || entry.Qty != 32 {
		t.Fatalf("Adjust +32 = %+v, %v", entry, err)
	}
	if entry, err = e.craft.Adjust(ctx, user, ws.ID, pearl.ID, -2.5, "проект Ель"); err != nil || entry.Qty != 29.5 {
		t.Fatalf("Adjust -2.5 = %+v, %v", entry, err)
	}
	if _, err = e.craft.Adjust(ctx, user, ws.ID, pearl.ID, -100, ""); !errors.Is(err, craft.ErrInsufficientStock) {
		t.Fatalf("overdraw = %v, want ErrInsufficientStock", err)
	}
	// The failed withdrawal must not have changed or logged anything.
	list, _ := e.craft.ListInventory(ctx, user, ws.ID, craft.CatalogFilter{})
	if len(list) != 1 || list[0].Qty != 29.5 {
		t.Fatalf("inventory after failed overdraw = %+v", list)
	}

	// Another workspace cannot see or touch the private item.
	if _, err := e.craft.Adjust(ctx, other, otherWS.ID, pearl.ID, 1, ""); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign item Adjust = %v, want ErrNotFound", err)
	}

	// Global items are read-only for workspaces but can be stocked.
	global := e.globalItem(t, "1152")
	if _, err := e.craft.UpdateItem(ctx, user, ws.ID, global, craft.ItemInput{Category: "seed-beads", Name: "x"}); !errors.Is(err, craft.ErrReadOnly) {
		t.Fatalf("update global = %v, want ErrReadOnly", err)
	}
	if _, err := e.craft.Adjust(ctx, user, ws.ID, global, 1, ""); err != nil {
		t.Fatal(err)
	}
	found, err := e.craft.SearchCatalog(ctx, user, ws.ID, craft.CatalogFilter{Query: "1152", OnlyInStock: true})
	if err != nil || len(found) != 1 || found[0].StockQty != 1 || !found[0].InInventory {
		t.Fatalf("search = %+v, %v", found, err)
	}

	// Link a private bead to the official one: stock is summed, private item gone.
	private, err := e.craft.CreateItem(ctx, user, ws.ID, craft.ItemInput{
		Category: "seed-beads", Manufacturer: "PRECIOSA Ornela", Name: "бисер светло-салатовый",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.craft.Adjust(ctx, user, ws.ID, private.ID, 2, ""); err != nil {
		t.Fatal(err)
	}
	linked, err := e.craft.LinkToCatalog(ctx, user, ws.ID, private.ID, global)
	if err != nil || linked.Qty != 3 {
		t.Fatalf("LinkToCatalog = %+v, %v; want qty 3", linked, err)
	}
	if _, err := e.craft.GetItem(ctx, user, ws.ID, private.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("private item must be deleted after linking, got %v", err)
	}
}
