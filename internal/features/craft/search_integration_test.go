package craft_test

import (
	"context"
	"slices"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// TestSearchWords proves that search ignores diacritics and the order of words
// and understands the Russian names of what catalogues write in English.
func TestSearchWords(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	home, err := e.workspaces.Create(ctx, user, workspace.AppWarehouse, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []craft.ItemInput{
		{Category: "floss", Manufacturer: "DMC", Article: "951", Name: "Mouliné Spécial 951", ColorName: "Tawny - LT", Unit: "моток"},
		{Category: "floss", Manufacturer: "Gamma", Article: "3160", Name: "Gamma 3160", ColorName: "светло-зелёный", Unit: "моток"},
		{Category: "seed-beads", Manufacturer: "PRECIOSA Ornela", Article: "331-19001-10_0-57120", Name: "Rocailles 10/0", ColorName: "transp. green, silver lined", Unit: "г"},
		{Category: "seed-beads", Name: "Бисер из набора", ColorName: "colored mix", Unit: "г"},
	} {
		item, err := e.craft.CreateItem(ctx, user, home.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.craft.Adjust(ctx, user, home.ID, item.ID, 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	const floss, gamma, beads, mix = "Mouliné Spécial 951", "Gamma 3160", "Rocailles 10/0", "Бисер из набора"
	for query, want := range map[string][]string{
		"Mouline Special":  {floss},
		"mouliné spécial":  {floss},
		"мулине":           {gamma, floss},
		"мули":             {gamma, floss},
		"мулине спешл 951": {floss},
		"951 мулине":       {floss},
		"дмс":              {floss},
		"гамма":            {gamma},
		"зеленый":          {gamma, beads},
		"зелёный бисер":    {beads},
		"красный":          {},
		"57120":            {beads},
		"пресиоза 10/0":    {beads},
		"серебро (":        {},
		"a.*":              {},
	} {
		rows, err := e.craft.ListInventory(ctx, user, home.ID, craft.CatalogFilter{Query: query})
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		got := []string{}
		for _, row := range rows {
			got = append(got, row.Item.Name)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("stock %q = %v, want %v", query, got, want)
		}
		found, err := e.craft.SearchCatalog(ctx, user, home.ID, craft.CatalogFilter{Query: query, OnlyInStock: true})
		if err != nil || len(found) != len(want) {
			t.Errorf("catalogue %q = %d items %v, want %d", query, len(found), err, len(want))
		}
	}
}
