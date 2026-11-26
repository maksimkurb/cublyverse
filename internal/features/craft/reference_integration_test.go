package craft_test

import (
	"context"
	"errors"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

func wantInvalid(t *testing.T, what string, err error) {
	t.Helper()
	var invalid *craft.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: err = %v, want a validation error", what, err)
	}
}

// TestWarehouseReference covers the configuration of a warehouse: templates,
// own categories with extra fields, manufacturers, units, isolation between
// warehouses and official catalogues.
func TestWarehouseReference(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	stranger := e.user(t, "stranger", "s@example.com")
	home, err := e.workspaces.Create(ctx, user, workspace.AppWarehouse, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	garage, err := e.workspaces.CreateFromTemplate(ctx, user, workspace.AppWarehouse, "Гараж", craft.TemplateEmpty)
	if err != nil {
		t.Fatal(err)
	}

	categories, _ := e.craft.Categories(ctx, user, home.ID)
	units, _ := e.craft.ListUnits(ctx, user, home.ID)
	if len(categories) != 13 || len(units) != 7 {
		t.Fatalf("craft template: %d categories, %d units", len(categories), len(units))
	}
	categories, _ = e.craft.Categories(ctx, user, garage.ID)
	units, _ = e.craft.ListUnits(ctx, user, garage.ID)
	if len(categories) != 0 || len(units) != 1 {
		t.Fatalf("empty template: %d categories, %d units", len(categories), len(units))
	}
	if _, err := e.craft.Categories(ctx, stranger, home.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("stranger = %v, want ErrNotFound", err)
	}

	// Own unit, category with fields, and an item that uses them.
	box, err := e.craft.SaveUnit(ctx, user, garage.ID, "", "коробка")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.craft.SaveUnit(ctx, user, garage.ID, "", "Коробка")
	wantInvalid(t, "duplicate unit", err)
	screws, err := e.craft.SaveCategory(ctx, user, garage.ID, "", craft.CategoryInput{
		Name: "Крепёж", DefaultUnit: "коробка",
		Fields: []craft.Field{
			{Label: "Длина, мм", Type: craft.FieldNumber},
			{Label: "Шлиц", Type: craft.FieldSelect, Options: []string{"крест", "звезда"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	length, slot := screws.Fields[0].Key, screws.Fields[1].Key
	input := craft.ItemInput{Category: "крепёж", Name: "Саморез", Manufacturer: "Метизы", Attrs: map[string]any{length: "длинный"}}
	_, err = e.craft.CreateItem(ctx, user, garage.ID, input)
	wantInvalid(t, "number field", err)
	input.Attrs = map[string]any{length: 35.0, slot: "плоский"}
	_, err = e.craft.CreateItem(ctx, user, garage.ID, input)
	wantInvalid(t, "select field", err)
	input.Attrs = map[string]any{length: 35.0, slot: "звезда", "imported": "kept"}
	item, err := e.craft.CreateItem(ctx, user, garage.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if item.Unit != "коробка" || item.Category != screws.Slug || item.CategoryName != "Крепёж" || item.Attrs["imported"] != "kept" {
		t.Fatalf("item = %+v", item)
	}
	_, err = e.craft.CreateItem(ctx, user, home.ID, craft.ItemInput{Category: screws.Slug, Name: "Чужая категория"})
	wantInvalid(t, "category of another warehouse", err)

	// Entries in use cannot go; a unit rename follows into items.
	wantInvalid(t, "category in use", e.craft.DeleteCategory(ctx, user, garage.ID, screws.ID))
	wantInvalid(t, "unit in use", e.craft.DeleteUnit(ctx, user, garage.ID, box.ID))
	if _, err := e.craft.SaveUnit(ctx, user, garage.ID, box.ID, "упак."); err != nil {
		t.Fatal(err)
	}
	renamed, err := e.craft.GetItem(ctx, user, garage.ID, item.ID)
	if err != nil || renamed.Unit != "упак." {
		t.Fatalf("renamed unit = %q (%v)", renamed.Unit, err)
	}
	makers, _ := e.craft.Manufacturers(ctx, user, garage.ID)
	if len(makers) != 1 || makers[0].Official || makers[0].Items != 1 {
		t.Fatalf("garage manufacturers = %+v", makers)
	}
	wantInvalid(t, "manufacturer in use", e.craft.DeleteManufacturer(ctx, user, garage.ID, makers[0].ID))
	if others, _ := e.craft.Manufacturers(ctx, user, home.ID); len(others) != 0 {
		t.Fatalf("another warehouse sees a private manufacturer: %+v", others)
	}
	if err := e.craft.DeleteCategory(ctx, user, home.ID, screws.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign category delete = %v", err)
	}
	if err := e.craft.DeleteItem(ctx, user, garage.ID, item.ID); err != nil {
		t.Fatal(err)
	}
	for what, err := range map[string]error{
		"category":     e.craft.DeleteCategory(ctx, user, garage.ID, screws.ID),
		"manufacturer": e.craft.DeleteManufacturer(ctx, user, garage.ID, makers[0].ID),
		"unit":         e.craft.DeleteUnit(ctx, user, garage.ID, box.ID),
	} {
		if err != nil {
			t.Fatalf("delete unused %s: %v", what, err)
		}
	}

	// Official items appear only in connected catalogues, under the
	// warehouse's own name of the category.
	official := e.globalItem(t, "311-19001")
	search := func(space string) []craft.Item {
		t.Helper()
		found, err := e.craft.SearchCatalog(ctx, user, space, craft.CatalogFilter{Query: "311-19001"})
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	if len(search(garage.ID)) != 0 {
		t.Fatal("a disconnected catalogue must be hidden")
	}
	if _, err := e.craft.SetCatalogSources(ctx, user, garage.ID, []string{"preciosa"}); err != nil {
		t.Fatal(err)
	}
	found := search(garage.ID)
	if len(found) != 1 || found[0].ID != official || found[0].CategoryName != "Бисер" {
		t.Fatalf("connected catalogue: %+v", found)
	}
	beads, _ := e.craft.Categories(ctx, user, garage.ID)
	if len(beads) != 1 || beads[0].CatalogSlug != "seed-beads" {
		t.Fatalf("connecting must add the category: %+v", beads)
	}
	if _, err := e.craft.SaveCategory(ctx, user, garage.ID, beads[0].ID, craft.CategoryInput{Name: "Мой бисер", DefaultUnit: "г"}); err != nil {
		t.Fatal(err)
	}
	if found = search(garage.ID); found[0].CategoryName != "Мой бисер" || search(home.ID)[0].CategoryName != "Бисер" {
		t.Fatalf("a renamed category is local: %+v", found)
	}
	_, err = e.craft.SetCatalogSources(ctx, user, garage.ID, []string{"gamma"})
	wantInvalid(t, "unknown catalogue", err)
}
