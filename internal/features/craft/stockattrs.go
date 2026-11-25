package craft

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// stockAttrs is what a warehouse remembers about a stocked item beyond the
// catalogue (craft_inventory.attrs).
type stockAttrs struct {
	// ColorName and ColorHex fill the gaps of an official item that has none.
	ColorName string `json:"color_name,omitempty"`
	ColorHex  string `json:"color_hex,omitempty"`
	// Legacy keeps the values the stock had before it was linked to this item.
	Legacy map[string]any `json:"legacy,omitempty"`
}

// withStockColors shows the remembered colour of the warehouse where the
// catalogue item has none. Values of the catalogue always win.
func withStockColors(item Item, raw []byte) Item {
	if len(raw) == 0 {
		return item
	}
	var attrs stockAttrs
	if json.Unmarshal(raw, &attrs) != nil {
		return item
	}
	if item.ColorName == "" {
		item.ColorName = attrs.ColorName
	}
	if item.ColorHex == "" && hexColor.MatchString(attrs.ColorHex) {
		item.ColorHex, item.ColorSource = attrs.ColorHex, "approx"
	}
	return item
}

// rememberLinked stores the data of a private item on the stock of the item it
// was linked to, so linking loses nothing.
func rememberLinked(ctx context.Context, q *db.Queries, wid, target pgtype.UUID, private db.CraftCatalogItem) error {
	legacy := map[string]any{
		"name": private.Name, "article": private.Article, "series": private.Series,
		"color_name": private.ColorName, "color_hex": private.ColorHex, "size": private.Size,
		"finish": private.Finish, "unit": private.Unit, "source": private.Source,
		"source_url": private.SourceUrl, "attrs": json.RawMessage(private.Attrs),
	}
	encoded, err := json.Marshal(stockAttrs{ColorName: private.ColorName, ColorHex: private.ColorHex, Legacy: legacy})
	if err != nil {
		return fmt.Errorf("encode linked item: %w", err)
	}
	if err := q.SetCraftInventoryAttrs(ctx, db.SetCraftInventoryAttrsParams{WorkspaceID: wid, CatalogItemID: target, Attrs: encoded}); err != nil {
		return fmt.Errorf("remember linked item: %w", err)
	}
	return nil
}
