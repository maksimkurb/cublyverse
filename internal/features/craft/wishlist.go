package craft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// WishStatuses are the states a wish list entry can be in.
var WishStatuses = []string{"wanted", "ordered", "bought"}

// WishFilter narrows the wish list. An empty Status means "wanted"; the
// special value "all" returns every status.
type WishFilter struct{ Status string }

// WishInput is a new wish list entry. A catalogue item is optional: without one
// the entry is free text for something that is not in any catalogue yet.
type WishInput struct {
	CatalogItemID string
	// Title defaults to the name of the catalogue item.
	Title     string
	QtyWanted *float64
	Priority  int
	Note      string
}

// WishUpdate replaces the editable fields of a wish list entry.
type WishUpdate struct {
	QtyWanted *float64
	Priority  int
	Note      string
	// Status defaults to the current status when empty.
	Status string
}

// WishLinkInput is a shop link of a wish list entry.
type WishLinkInput struct {
	URL      string
	ShopName string
	Price    *float64
	Currency string
	InStock  *bool
}

// WishLink is one shop page where a wish can be bought.
type WishLink struct {
	ID        string
	URL       string
	ShopName  string
	Price     *float64
	Currency  string
	InStock   *bool
	CheckedAt *time.Time
	CreatedAt time.Time
}

// Wish is a wish list entry with its links and, when it points at a catalogue
// item, the item summary and the current stock of the workspace.
type Wish struct {
	ID            string
	CatalogItemID string
	Title         string
	QtyWanted     *float64
	Priority      int
	Note          string
	Status        string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Item          *Item
	Links         []WishLink
}

// GetWish returns one wish list entry of the workspace.
func (s *Service) GetWish(ctx context.Context, userID, workspaceID, wishID string) (Wish, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Wish{}, err
	}
	return s.loadWish(ctx, s.st.Queries(), wid, wishID)
}

// AddWish adds an entry to the wish list. When CatalogItemID is set the entry
// points at a catalogue item visible to the workspace; a catalogue item can be
// wanted at most once at a time.
func (s *Service) AddWish(ctx context.Context, userID, workspaceID string, in WishInput) (Wish, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Wish{}, err
	}
	actor, _ := workspace.ParseID(userID)
	var created db.CraftWishlistItem
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		var itemID pgtype.UUID
		title := strings.TrimSpace(in.Title)
		if in.CatalogItemID != "" {
			var err error
			itemID, err = parseItem(in.CatalogItemID)
			if err != nil {
				return err
			}
			item, err := s.visible(ctx, q, wid, itemID)
			if err != nil {
				return err
			}
			if title == "" {
				title = item.Name
			}
		}
		if title == "" {
			return invalid("Укажите название позиции.")
		}
		if utf8.RuneCountInString(title) > 200 {
			return invalid("Название длиннее 200 символов.")
		}
		qty, err := wishQty(in.QtyWanted)
		if err != nil {
			return err
		}
		if in.Priority < 0 || in.Priority > 2 {
			return invalid("Приоритет: 0, 1 или 2.")
		}
		created, err = q.CreateCraftWishlistItem(ctx, db.CreateCraftWishlistItemParams{
			WorkspaceID: wid, CatalogItemID: itemID, Title: title, QtyWanted: qty,
			Priority: int16(in.Priority), Note: strings.TrimSpace(in.Note), CreatedBy: actor,
		})
		return mapWishUnique(err)
	})
	if err != nil {
		return Wish{}, err
	}
	return s.loadWish(ctx, s.st.Queries(), wid, workspace.FormatID(created.ID))
}

// ListWishes lists the wish list of the workspace. The default status is
// "wanted"; "all" returns every entry.
func (s *Service) ListWishes(ctx context.Context, userID, workspaceID string, f WishFilter) ([]Wish, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	status, err := wishStatusFilter(f.Status)
	if err != nil {
		return nil, err
	}
	q := s.st.Queries()
	rows, err := q.ListCraftWishlistItems(ctx, db.ListCraftWishlistItemsParams{
		WorkspaceID: wid, Status: status,
	})
	if err != nil {
		return nil, fmt.Errorf("list wishes: %w", err)
	}
	out := make([]Wish, 0, len(rows))
	for _, row := range rows {
		out = append(out, wishFromRow(row))
	}
	links, err := q.ListCraftWishlistLinksForWorkspace(ctx, db.ListCraftWishlistLinksForWorkspaceParams{
		WorkspaceID: wid, Status: status,
	})
	if err != nil {
		return nil, fmt.Errorf("list wish links: %w", err)
	}
	byWish := make(map[string][]WishLink, len(out))
	for _, link := range links {
		id := workspace.FormatID(link.WishlistItemID)
		byWish[id] = append(byWish[id], wishLinkFromRow(link))
	}
	for i := range out {
		out[i].Links = byWish[out[i].ID]
	}
	return out, nil
}

// UpdateWish replaces the quantity, priority, note and status of an entry.
// An empty status keeps the current one.
func (s *Service) UpdateWish(ctx context.Context, userID, workspaceID, wishID string, in WishUpdate) (Wish, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Wish{}, err
	}
	pid, err := parseWish(wishID)
	if err != nil {
		return Wish{}, err
	}
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		existing, err := s.loadWishRow(ctx, q, wid, pid)
		if err != nil {
			return err
		}
		qty, err := wishQty(in.QtyWanted)
		if err != nil {
			return err
		}
		if in.Priority < 0 || in.Priority > 2 {
			return invalid("Приоритет: 0, 1 или 2.")
		}
		status := in.Status
		if status == "" {
			status = existing.Status
		}
		if !validWishStatus(status) {
			return invalid("Статус: wanted, ordered или bought.")
		}
		_, err = q.UpdateCraftWishlistItem(ctx, db.UpdateCraftWishlistItemParams{
			ID: pid, WorkspaceID: wid, QtyWanted: qty, Priority: int16(in.Priority),
			Note: strings.TrimSpace(in.Note), Status: status,
		})
		return mapWishUnique(err)
	})
	if err != nil {
		return Wish{}, err
	}
	return s.loadWish(ctx, s.st.Queries(), wid, wishID)
}

// DeleteWish removes a wish list entry and its links.
func (s *Service) DeleteWish(ctx context.Context, userID, workspaceID, wishID string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	pid, err := parseWish(wishID)
	if err != nil {
		return err
	}
	rows, err := s.st.Queries().DeleteCraftWishlistItem(ctx, db.DeleteCraftWishlistItemParams{ID: pid, WorkspaceID: wid})
	if err != nil {
		return fmt.Errorf("delete wish: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// AddWishLink adds a shop link to an entry. The URL must be http or https.
func (s *Service) AddWishLink(ctx context.Context, userID, workspaceID, wishID string, in WishLinkInput) (WishLink, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return WishLink{}, err
	}
	pid, err := parseWish(wishID)
	if err != nil {
		return WishLink{}, err
	}
	linkURL, err := canonicalLinkURL(in.URL)
	if err != nil {
		return WishLink{}, err
	}
	if in.Price != nil && (*in.Price < 0 || math.IsNaN(*in.Price) || math.IsInf(*in.Price, 0)) {
		return WishLink{}, invalid("Цена не может быть отрицательной.")
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.ShopName)) > 120 {
		return WishLink{}, invalid("Название магазина длиннее 120 символов.")
	}
	var price pgtype.Numeric
	if in.Price != nil {
		price = numeric(*in.Price)
	}
	var created db.CraftWishlistLink
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		if _, err := s.loadWishRow(ctx, q, wid, pid); err != nil {
			return err
		}
		row, err := q.CreateCraftWishlistLink(ctx, db.CreateCraftWishlistLinkParams{
			WishlistItemID: pid, Url: linkURL, ShopName: strings.TrimSpace(in.ShopName),
			Price: price, Currency: strings.ToUpper(strings.TrimSpace(in.Currency)), InStock: in.InStock,
		})
		if err != nil {
			return fmt.Errorf("add wish link: %w", err)
		}
		created = row
		return nil
	})
	if err != nil {
		return WishLink{}, err
	}
	return wishLinkFromRow(created), nil
}

// RemoveWishLink removes one shop link of an entry.
func (s *Service) RemoveWishLink(ctx context.Context, userID, workspaceID, wishID, linkID string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	pid, err := parseWish(wishID)
	if err != nil {
		return err
	}
	lid, err := parseWish(linkID)
	if err != nil {
		return err
	}
	rows, err := s.st.Queries().DeleteCraftWishlistLink(ctx, db.DeleteCraftWishlistLinkParams{
		ID: lid, WorkspaceID: wid, WishlistItemID: pid,
	})
	if err != nil {
		return fmt.Errorf("remove wish link: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkBought marks an entry as bought. When the entry points at a catalogue
// item and addToStock is set, the wanted quantity is added to the stock in the
// same transaction, exactly as Adjust would. A second call on an already bought
// entry changes no stock.
func (s *Service) MarkBought(ctx context.Context, userID, workspaceID, wishID string, addToStock bool) (Wish, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Wish{}, err
	}
	pid, err := parseWish(wishID)
	if err != nil {
		return Wish{}, err
	}
	actor, _ := workspace.ParseID(userID)
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		existing, err := s.loadWishRow(ctx, q, wid, pid)
		if err != nil {
			return err
		}
		if existing.Status == "bought" {
			return nil
		}
		if addToStock && existing.CatalogItemID.Valid {
			if existing.QtyWanted.Valid {
				qty := numFloat(existing.QtyWanted)
				if _, err := s.adjustInTx(ctx, q, wid, existing.CatalogItemID, actor, pgtype.UUID{},
					qty, "куплено по списку закупок"); err != nil {
					return err
				}
			} else {
				return invalid("Укажите количество, чтобы добавить покупку в запас.")
			}
		}
		_, err = q.SetCraftWishlistStatus(ctx, db.SetCraftWishlistStatusParams{
			ID: pid, WorkspaceID: wid, Status: "bought",
		})
		return err
	})
	if err != nil {
		return Wish{}, err
	}
	return s.loadWish(ctx, s.st.Queries(), wid, wishID)
}

// --- internals -------------------------------------------------------------

func parseWish(id string) (pgtype.UUID, error) {
	u, err := workspace.ParseID(id)
	if err != nil {
		return pgtype.UUID{}, ErrNotFound
	}
	return u, nil
}

func wishQty(qty *float64) (pgtype.Numeric, error) {
	if qty == nil {
		return pgtype.Numeric{}, nil
	}
	if *qty <= 0 || math.IsNaN(*qty) || math.IsInf(*qty, 0) {
		return pgtype.Numeric{}, invalid("Количество должно быть положительным.")
	}
	return numeric(*qty), nil
}

func validWishStatus(status string) bool {
	for _, s := range WishStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// wishStatusFilter maps the public filter to the query argument: the default is
// "wanted", "all" means an empty string (any status).
func wishStatusFilter(status string) (string, error) {
	if status == "" {
		return "wanted", nil
	}
	if status == "all" {
		return "", nil
	}
	if !validWishStatus(status) {
		return "", invalid("Статус: wanted, ordered или bought.")
	}
	return status, nil
}

func canonicalLinkURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", invalid("Ссылка должна начинаться с http:// или https://.")
	}
	return trimmed, nil
}

// loadWish returns one entry with its catalogue item and links.
func (s *Service) loadWish(ctx context.Context, q *db.Queries, wid pgtype.UUID, wishID string) (Wish, error) {
	pid, err := parseWish(wishID)
	if err != nil {
		return Wish{}, err
	}
	row, err := s.loadWishRow(ctx, q, wid, pid)
	if err != nil {
		return Wish{}, err
	}
	wish := wishFromRow(row)
	links, err := q.ListCraftWishlistLinks(ctx, pid)
	if err != nil {
		return Wish{}, fmt.Errorf("list wish links: %w", err)
	}
	for _, link := range links {
		wish.Links = append(wish.Links, wishLinkFromRow(link))
	}
	return wish, nil
}

func (s *Service) loadWishRow(ctx context.Context, q *db.Queries, wid, pid pgtype.UUID) (db.ListCraftWishlistItemsRow, error) {
	rows, err := q.ListCraftWishlistItems(ctx, db.ListCraftWishlistItemsParams{
		WorkspaceID: wid, WishID: pid,
	})
	if err != nil {
		return db.ListCraftWishlistItemsRow{}, fmt.Errorf("get wish: %w", err)
	}
	if len(rows) == 0 {
		return db.ListCraftWishlistItemsRow{}, ErrNotFound
	}
	return rows[0], nil
}

func wishFromRow(r db.ListCraftWishlistItemsRow) Wish {
	w := Wish{
		ID: workspace.FormatID(r.ID), CatalogItemID: workspace.FormatID(r.CatalogItemID),
		Title: r.Title, QtyWanted: numFloatPtr(r.QtyWanted), Priority: int(r.Priority),
		Note: r.Note, Status: r.Status, CreatedBy: workspace.FormatID(r.CreatedBy),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.ItemID.Valid {
		attrs := map[string]any{}
		_ = json.Unmarshal(r.ItemAttrs, &attrs)
		item := Item{
			ID: workspace.FormatID(r.ItemID), WorkspaceID: workspace.FormatID(r.ItemWorkspaceID),
			Category: r.CategorySlug, CategoryName: r.CategoryName,
			Manufacturer: deref(r.ManufacturerName), Article: deref(r.ItemArticle),
			Series: deref(r.ItemSeries), Name: deref(r.ItemName), ColorName: deref(r.ItemColorName),
			ColorHex: deref(r.ItemColorHex), ColorSource: deref(r.ItemColorSource),
			Size: deref(r.ItemSize), Finish: deref(r.ItemFinish), Unit: deref(r.ItemUnit),
			PackSize: numFloatPtr(r.ItemPackSize), Attrs: attrs, Source: deref(r.ItemSource),
			SourceURL: deref(r.ItemSourceUrl), ImageURL: deref(r.ItemImageUrl),
			StockQty: numFloat(r.StockQty), InInventory: r.InInventory, shopHex: deref(r.ItemShopColorHex), photoHex: deref(r.ItemPhotoColorHex),
		}
		item = withStockColors(item, r.StockAttrs)
		w.Item = &item
	}
	return w
}

func wishLinkFromRow(r db.CraftWishlistLink) WishLink {
	var checkedAt *time.Time
	if r.CheckedAt.Valid {
		t := r.CheckedAt.Time
		checkedAt = &t
	}
	return WishLink{
		ID: workspace.FormatID(r.ID), URL: r.Url, ShopName: r.ShopName,
		Price: numFloatPtr(r.Price), Currency: r.Currency, InStock: r.InStock,
		CheckedAt: checkedAt, CreatedAt: r.CreatedAt,
	}
}

// mapWishUnique turns the "one wanted entry per catalogue item" violation into
// a client error.
func mapWishUnique(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return invalid("Позиция уже в закупках")
	}
	return fmt.Errorf("save wish: %w", err)
}
