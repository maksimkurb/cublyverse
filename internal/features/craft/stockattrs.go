package craft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// stockAttrs is what a warehouse remembers about a stocked item beyond the
// catalogue (craft_inventory.attrs).
type stockAttrs struct {
	// ColorName and ColorHex fill the gaps of an official item that has none.
	ColorName string `json:"color_name,omitempty"`
	ColorHex  string `json:"color_hex,omitempty"`
	// ColorOrigin says where a colour set by a person or a model came from.
	ColorOrigin string `json:"color_origin,omitempty"`
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
		"source_url": private.SourceUrl, "image_url": private.ImageUrl, "attrs": json.RawMessage(private.Attrs),
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

// ColorInput is a colour somebody found for an item.
type ColorInput struct {
	ColorHex  string
	ColorName string
	// Origin says where the colour comes from, for example a page address.
	Origin string
}

// SetColor records an approximate colour of an item for one warehouse. An own
// item is changed itself. An official item is shared and read-only, so the
// colour is kept on the warehouse's stock row and shown only while the
// catalogue has none.
func (s *Service) SetColor(ctx context.Context, userID, workspaceID, itemID string, in ColorInput) (Item, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Item{}, err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return Item{}, err
	}
	hex := strings.ToUpper(strings.TrimSpace(in.ColorHex))
	if !hexColor.MatchString(hex) {
		return Item{}, invalid("Цвет должен быть в формате #RRGGBB.")
	}
	name, origin := strings.TrimSpace(in.ColorName), strings.TrimSpace(in.Origin)
	if origin == "" || len(origin) > 500 {
		return Item{}, invalid("Укажите источник цвета (до 500 символов).")
	}
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		item, err := s.visible(ctx, q, wid, iid)
		if err != nil {
			return err
		}
		if item.WorkspaceID.Valid {
			attrs := map[string]any{}
			_ = json.Unmarshal(item.Attrs, &attrs)
			attrs["color_origin"] = origin
			encoded, err := json.Marshal(attrs)
			if err != nil {
				return fmt.Errorf("encode attributes: %w", err)
			}
			return q.SetCraftItemColor(ctx, db.SetCraftItemColorParams{
				ID: iid, WorkspaceID: wid, ColorHex: hex, ColorName: name, Attrs: encoded,
			})
		}
		if item.ColorHex != "" {
			return invalid("В официальном каталоге у позиции уже есть цвет %s.", item.ColorHex)
		}
		stock, err := q.LockCraftInventoryRow(ctx, db.LockCraftInventoryRowParams{WorkspaceID: wid, CatalogItemID: iid})
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("Цвет официальной позиции хранится вместе с остатком: сначала добавьте её в запасы.")
		}
		if err != nil {
			return fmt.Errorf("lock stock: %w", err)
		}
		var attrs stockAttrs
		_ = json.Unmarshal(stock.Attrs, &attrs)
		attrs.ColorHex, attrs.ColorOrigin = hex, origin
		if name != "" {
			attrs.ColorName = name
		}
		encoded, err := json.Marshal(attrs)
		if err != nil {
			return fmt.Errorf("encode stock attributes: %w", err)
		}
		return q.SetCraftInventoryAttrs(ctx, db.SetCraftInventoryAttrsParams{WorkspaceID: wid, CatalogItemID: iid, Attrs: encoded})
	})
	if err != nil {
		return Item{}, err
	}
	return s.loadItem(ctx, s.st.Queries(), wid, itemID)
}
