package craft_test

import (
	"context"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// TestLinkKeepsColour proves that linking an own item to an official one that
// has no colour keeps the warehouse's colour, everywhere the item is shown.
func TestLinkKeepsColour(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	home, err := e.workspaces.Create(ctx, user, workspace.AppWarehouse, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	official := e.globalItem(t, "311-19001")
	private, err := e.craft.CreateItem(ctx, user, home.ID, craft.ItemInput{
		Category: "seed-beads", Name: "Старый бисер", ColorName: "салатовый", ColorHex: "#A9C33E", Unit: "упаковка",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.craft.Adjust(ctx, user, home.ID, private.ID, 3, ""); err != nil {
		t.Fatal(err)
	}
	linked, err := e.craft.LinkToCatalog(ctx, user, home.ID, private.ID, official)
	if err != nil {
		t.Fatal(err)
	}
	check := func(where string, item craft.Item) {
		t.Helper()
		if item.ColorHex != "#A9C33E" || item.ColorName != "салатовый" || item.ColorSource != "approx" {
			t.Fatalf("%s: colour = %q %q %q", where, item.ColorHex, item.ColorName, item.ColorSource)
		}
		if item.Name != "PRECIOSA 311-19001" {
			t.Fatalf("%s: the official name must win, got %q", where, item.Name)
		}
	}
	check("link", linked.Item)
	stock, err := e.craft.ListInventory(ctx, user, home.ID, craft.CatalogFilter{Query: "салат"})
	if err != nil || len(stock) != 1 {
		t.Fatalf("search by the remembered colour: %v %v", stock, err)
	}
	check("inventory", stock[0].Item)
	found, err := e.craft.SearchCatalog(ctx, user, home.ID, craft.CatalogFilter{Query: "311-19001"})
	if err != nil || len(found) != 1 {
		t.Fatalf("catalogue: %v %v", found, err)
	}
	check("catalogue", found[0])
	wish, err := e.craft.AddWish(ctx, user, home.ID, craft.WishInput{CatalogItemID: official})
	if err != nil || wish.Item == nil {
		t.Fatalf("wish: %v", err)
	}
	check("wish", *wish.Item)

	// A colour found later replaces the remembered one and keeps the old values.
	if _, err := e.craft.SetColor(ctx, user, home.ID, official, craft.ColorInput{ColorHex: "#abcdef"}); err == nil {
		t.Fatal("a colour without a source must be refused")
	}
	recoloured, err := e.craft.SetColor(ctx, user, home.ID, official, craft.ColorInput{ColorHex: "#abcdef", Origin: "https://example.com"})
	if err != nil || recoloured.ColorHex != "#ABCDEF" || recoloured.ColorName != "салатовый" {
		t.Fatalf("set colour: %+v %v", recoloured, err)
	}
	var kept string
	if err := e.st.Pool().QueryRow(ctx, "SELECT attrs->'legacy'->>'name' FROM craft_inventory").Scan(&kept); err != nil || kept != "Старый бисер" {
		t.Fatalf("legacy values lost: %q %v", kept, err)
	}
	other := e.globalItem(t, "311-19002")
	_, err = e.craft.SetColor(ctx, user, home.ID, other, craft.ColorInput{ColorHex: "#000000", Origin: "тест"})
	wantInvalid(t, "official item outside the stock", err)
}
