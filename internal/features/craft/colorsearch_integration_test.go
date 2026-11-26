package craft_test

import (
	"context"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// TestColorSearch proves that a colour search sees the colour the warehouse is
// shown — published, remembered or read from the photo — and that a photo
// colour gives way to a better one.
func TestColorSearch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	home, err := e.workspaces.Create(ctx, user, workspace.AppWarehouse, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	official := e.globalItem(t, "311-19001")
	if _, err := e.st.Pool().Exec(ctx, "UPDATE craft_catalog_items SET photo_color_hex = '#C01020' WHERE id = $1", official); err != nil {
		t.Fatal(err)
	}
	own := map[string]string{}
	for name, hex := range map[string]string{"синий": "#1030C0", "алый": "#E02028", "без цвета": ""} {
		item, err := e.craft.CreateItem(ctx, user, home.ID, craft.ItemInput{
			Category: "seed-beads", Name: name, ColorHex: hex, Unit: "упаковка",
		})
		if err != nil {
			t.Fatal(err)
		}
		own[name] = item.ID
		if _, err := e.craft.Adjust(ctx, user, home.ID, item.ID, 1, ""); err != nil {
			t.Fatal(err)
		}
	}

	found, err := e.craft.SearchCatalog(ctx, user, home.ID, craft.CatalogFilter{Color: "#c01020"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 || found[0].ID != official || found[1].ID != own["алый"] || found[2].ID != own["синий"] {
		t.Fatalf("catalogue by colour = %+v", found)
	}
	if found[0].ColorSource != "photo" || found[0].ColorHex != "#C01020" {
		t.Fatalf("photo colour = %q %q", found[0].ColorHex, found[0].ColorSource)
	}
	second, err := e.craft.SearchCatalog(ctx, user, home.ID, craft.CatalogFilter{Color: "#c01020", Limit: 1, Offset: 1})
	if err != nil || len(second) != 1 || second[0].ID != own["алый"] {
		t.Fatalf("second page = %+v %v", second, err)
	}
	stock, err := e.craft.ListInventory(ctx, user, home.ID, craft.CatalogFilter{Color: "#0020FF"})
	if err != nil || len(stock) != 2 || stock[0].Item.ID != own["синий"] {
		t.Fatalf("stock by colour = %+v %v", stock, err)
	}
	_, err = e.craft.ListInventory(ctx, user, home.ID, craft.CatalogFilter{Color: "red"})
	wantInvalid(t, "a colour that is not #RRGGBB", err)

	if _, err := e.craft.Adjust(ctx, user, home.ID, official, 1, ""); err != nil {
		t.Fatal(err)
	}
	// The stock list shows the product photo too.
	if _, err := e.st.Pool().Exec(ctx, "UPDATE craft_catalog_items SET image_url = 'https://example.com/p.jpg' WHERE id = $1", official); err != nil {
		t.Fatal(err)
	}
	stocked, err := e.craft.ListInventory(ctx, user, home.ID, craft.CatalogFilter{Query: "311-19001"})
	if err != nil || len(stocked) != 1 || stocked[0].Item.ImageURL != "https://example.com/p.jpg" {
		t.Fatalf("stock photo = %+v %v", stocked, err)
	}
	better, err := e.craft.SetColor(ctx, user, home.ID, official, craft.ColorInput{ColorHex: "#AA0011", Origin: "тест"})
	if err != nil || better.ColorHex != "#AA0011" || better.ColorSource != "approx" {
		t.Fatalf("a remembered colour must replace the photo colour: %+v %v", better, err)
	}
}
