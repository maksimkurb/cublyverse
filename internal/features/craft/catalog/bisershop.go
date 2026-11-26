package catalog

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// DefaultBiserShopURL is a shop's table that matches PRECIOSA colour numbers to
// other brands and gives some of them an approximate RGB value.
const DefaultBiserShopURL = "https://www.biser-shop.ru/preciosa_biser.html"

// BiserShopSource names the table in craft_catalog_sync_runs.
const BiserShopSource = "biser-shop"

// minShopColors rejects a page that lost most of the table, for example after a
// redesign: applying it would erase the colours already stored.
const minShopColors = 200

var (
	tableRow    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	tableCell   = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	htmlTag     = regexp.MustCompile(`<[^>]*>`)
	shopCode    = regexp.MustCompile(`^\d{5}m?$`)
	articleCode = regexp.MustCompile(`-(\d{5}m?)$`)
)

// BiserShop reads the colour table. It is not a catalogue: it only adds colours
// to PRECIOSA items that already exist.
type BiserShop struct {
	client *http.Client
	// URL is the page with the table.
	URL string
	// MinColors is the smallest table that is believed.
	MinColors int
}

// NewBiserShop returns the reader. A nil client becomes NewHTTPClient().
func NewBiserShop(client *http.Client) *BiserShop {
	if client == nil {
		client = NewHTTPClient()
	}
	return &BiserShop{client: client, URL: DefaultBiserShopURL, MinColors: minShopColors}
}

// Colors downloads the table and returns colour number -> #RRGGBB.
func (b *BiserShop) Colors(ctx context.Context) (map[string]string, error) {
	page, err := get(ctx, b.client, b.URL)
	if err != nil {
		return nil, err
	}
	colors := parseShopColors(page)
	if len(colors) < b.MinColors {
		return nil, fmt.Errorf("biser-shop: the table has only %d colours, want at least %d; the page changed", len(colors), b.MinColors)
	}
	return colors, nil
}

// parseShopColors finds the columns by their headings, so a column added to
// the table does not turn another brand's number into a colour. The first value
// of a repeated number wins.
func parseShopColors(page []byte) map[string]string {
	colors := make(map[string]string)
	codeColumn, rgbColumn := -1, -1
	for _, row := range tableRow.FindAllSubmatch(page, -1) {
		var cells []string
		for _, cell := range tableCell.FindAllSubmatch(row[1], -1) {
			text := html.UnescapeString(htmlTag.ReplaceAllString(string(cell[1]), ""))
			cells = append(cells, strings.TrimSpace(strings.ReplaceAll(text, " ", " ")))
		}
		code, rgb := -1, -1
		for i, cell := range cells {
			if strings.EqualFold(cell, "PRECIOSA") {
				code = i
			}
			if strings.Contains(strings.ToUpper(cell), "RGB") {
				rgb = i
			}
		}
		if code >= 0 && rgb >= 0 {
			codeColumn, rgbColumn = code, rgb
			continue
		}
		if codeColumn < 0 || len(cells) <= max(codeColumn, rgbColumn) || !shopCode.MatchString(cells[codeColumn]) {
			continue
		}
		hex := normalizeHex("#" + strings.TrimPrefix(cells[rgbColumn], "#"))
		if _, seen := colors[cells[codeColumn]]; hex != "" && !seen {
			colors[cells[codeColumn]] = hex
		}
	}
	return colors
}

// RunShopColors stores the table's colours on the PRECIOSA items with the same
// colour number, whatever their shape and size. The run is recorded like a
// catalogue sync; a failed one changes nothing.
func (s *Syncer) RunShopColors(ctx context.Context, shop *BiserShop) (Result, error) {
	if shop == nil {
		return Result{}, errors.New("catalog: nil colour table")
	}
	res := Result{Source: BiserShopSource}
	run, err := s.st.Queries().CreateCraftCatalogSyncRun(ctx, res.Source)
	if err != nil {
		return res, fmt.Errorf("catalog: start sync run: %w", err)
	}
	res.RunID = run.ID
	finish := func(status, message string) {
		if _, err := s.st.Queries().FinishCraftCatalogSyncRun(context.WithoutCancel(ctx), db.FinishCraftCatalogSyncRunParams{
			ID: res.RunID, Status: status, Updated: int32(res.Updated), Unchanged: int32(res.Unchanged),
			Error: truncateRunes(message, maxRunError),
		}); err != nil {
			s.logger.Error("catalog: finish sync run", slog.String("source", res.Source), slog.String("error", err.Error()))
		}
	}

	colors, err := shop.Colors(ctx)
	if err != nil {
		finish("failed", err.Error())
		return res, fmt.Errorf("catalog: fetch %s: %w", res.Source, err)
	}
	var updated, unchanged int
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		items, err := q.ListCraftShopColorTargets(ctx, "preciosa")
		if err != nil {
			return fmt.Errorf("list items: %w", err)
		}
		for _, item := range items {
			hex := ""
			if match := articleCode.FindStringSubmatch(item.Article); match != nil {
				hex = colors[match[1]]
			}
			if hex == item.ShopColorHex {
				if hex != "" {
					unchanged++
				}
				continue
			}
			if err := q.SetCraftShopColor(ctx, db.SetCraftShopColorParams{ID: item.ID, ShopColorHex: hex}); err != nil {
				return fmt.Errorf("save colour of %q: %w", item.Article, err)
			}
			updated++
		}
		return nil
	})
	if err != nil {
		finish("failed", err.Error())
		return res, fmt.Errorf("catalog: apply %s: %w", res.Source, err)
	}
	res.Updated, res.Unchanged = updated, unchanged
	finish("ok", "")
	s.logger.Info("catalog: shop colours applied", slog.Int("colors", len(colors)),
		slog.Int("updated", updated), slog.Int("unchanged", unchanged))
	return res, nil
}
