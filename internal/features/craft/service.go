// Package craft implements the craft supplies feature ("Бусинки"): a catalogue
// of materials, the stock a workspace holds, its projects and a wish list.
//
// Authorisation is by workspace membership: every method takes the id of the
// acting user and the workspace and first calls workspace.Service.Require, so a
// foreign workspace is indistinguishable from an unknown one. Catalogue items
// are either global (read-only here, written by scrapers) or private to the
// workspace.
package craft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// Units are the measurement units an item can be counted in.
var Units = []string{"шт", "г", "моток", "м", "см", "упаковка", "набор"}

// Page size bounds for list endpoints.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

var (
	// ErrNotFound means the object does not exist or is not visible to the
	// workspace.
	ErrNotFound = errors.New("craft: not found")
	// ErrReadOnly means the catalogue item is global and cannot be edited by a
	// workspace.
	ErrReadOnly = errors.New("craft: global catalogue item is read-only")
	// ErrInsufficientStock means a withdrawal exceeds the stock.
	ErrInsufficientStock = errors.New("craft: not enough stock")
)

// ValidationError is a client mistake with a message fit for the user.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Category is a kind of material.
type Category struct {
	ID          string
	Slug        string
	Name        string
	DefaultUnit string
	AttrSchema  json.RawMessage
}

// Manufacturer is a producer of materials.
type Manufacturer struct {
	ID      string
	Name    string
	Website string
}

// Item is a catalogue entry.
type Item struct {
	ID string
	// WorkspaceID is empty for a global item and the workspace id for a
	// private one.
	WorkspaceID  string
	Category     string // slug
	CategoryName string
	Manufacturer string
	Article      string
	Series       string
	Name         string
	ColorName    string
	ColorHex     string
	// ColorSource is "official", "approx" or empty.
	ColorSource string
	Size        string
	Finish      string
	Unit        string
	PackSize    *float64
	Attrs       map[string]any
	Source      string
	SourceURL   string
	ImageURL    string
	// StockQty and InInventory describe the workspace's stock of this item;
	// they are only set by catalogue search.
	StockQty    float64
	InInventory bool
}

// Private reports whether the item belongs to one workspace.
func (i Item) Private() bool { return i.WorkspaceID != "" }

// ItemInput is the data of a private catalogue item.
type ItemInput struct {
	// Category is the category slug.
	Category string
	// Manufacturer is the manufacturer name; empty for none. An unknown name is
	// created.
	Manufacturer string
	Article      string
	Series       string
	Name         string
	ColorName    string
	ColorHex     string
	ColorSource  string
	Size         string
	Finish       string
	// Unit defaults to the default unit of the category.
	Unit     string
	PackSize *float64
	Attrs    map[string]any
	// Source records where the data came from; defaults to "manual".
	Source    string
	SourceURL string
}

// StockEntry is one inventory row with its catalogue item.
type StockEntry struct {
	Item      Item
	Qty       float64
	Location  string
	Notes     string
	UpdatedAt time.Time
}

// CatalogFilter narrows a catalogue search or an inventory list. Empty strings
// mean "any".
type CatalogFilter struct {
	Category     string // slug
	Manufacturer string // name, case-insensitive exact match
	// Query matches an article prefix, or a part of the name or colour name.
	Query string
	// OnlyInStock limits catalogue search to items the workspace holds.
	OnlyInStock bool
	// MaxQty limits the inventory list to rows with qty <= MaxQty (low stock).
	MaxQty *float64
	Limit  int
	Offset int
}

// Service is the craft business logic.
type Service struct {
	st         *store.Store
	workspaces *workspace.Service
}

// New returns the service.
func New(st *store.Store, workspaces *workspace.Service) *Service {
	return &Service{st: st, workspaces: workspaces}
}

// Categories lists the material categories.
func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.st.Queries().ListCraftCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	out := make([]Category, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCategory(r))
	}
	return out, nil
}

// Manufacturers lists the known manufacturers.
func (s *Service) Manufacturers(ctx context.Context) ([]Manufacturer, error) {
	rows, err := s.st.Queries().ListCraftManufacturers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manufacturers: %w", err)
	}
	out := make([]Manufacturer, 0, len(rows))
	for _, r := range rows {
		out = append(out, Manufacturer{ID: workspace.FormatID(r.ID), Name: r.Name, Website: r.Website})
	}
	return out, nil
}

// SearchCatalog searches the global catalogue and the workspace's own items.
func (s *Service) SearchCatalog(ctx context.Context, userID, workspaceID string, f CatalogFilter) ([]Item, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	limit, offset := page(f.Limit, f.Offset)
	rows, err := s.st.Queries().SearchCraftCatalog(ctx, db.SearchCraftCatalogParams{
		WorkspaceID:  wid,
		Category:     f.Category,
		Manufacturer: f.Manufacturer,
		Query:        strings.TrimSpace(f.Query),
		OnlyInStock:  f.OnlyInStock,
		PageLimit:    limit,
		PageOffset:   offset,
	})
	if err != nil {
		return nil, fmt.Errorf("search catalogue: %w", err)
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		item := itemFromRow(db.CraftCatalogItem{
			ID: r.ID, WorkspaceID: r.WorkspaceID, Article: r.Article, Series: r.Series, Name: r.Name,
			ColorName: r.ColorName, ColorHex: r.ColorHex, ColorSource: r.ColorSource, Size: r.Size,
			Finish: r.Finish, Unit: r.Unit, PackSize: r.PackSize, Attrs: r.Attrs, Source: r.Source,
			SourceUrl: r.SourceUrl, ImageUrl: r.ImageUrl,
		}, r.CategorySlug, r.CategoryName, deref(r.ManufacturerName))
		item.StockQty = numFloat(r.StockQty)
		item.InInventory = r.InInventory
		out = append(out, item)
	}
	return out, nil
}

// GetItem returns a catalogue item visible to the workspace.
func (s *Service) GetItem(ctx context.Context, userID, workspaceID, itemID string) (Item, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Item{}, err
	}
	return s.loadItem(ctx, s.st.Queries(), wid, itemID)
}

// CreateItem adds a private catalogue item to the workspace.
func (s *Service) CreateItem(ctx context.Context, userID, workspaceID string, in ItemInput) (Item, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Item{}, err
	}
	var created db.CraftCatalogItem
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		params, err := s.itemParams(ctx, q, in)
		if err != nil {
			return err
		}
		created, err = q.CreateCraftCatalogItem(ctx, db.CreateCraftCatalogItemParams{
			WorkspaceID: wid, CategoryID: params.category.ID, ManufacturerID: params.manufacturerID,
			Article: params.article, Series: in.Series, Name: params.name,
			ColorName: in.ColorName, ColorHex: params.colorHex, ColorSource: params.colorSource,
			Size: in.Size, Finish: in.Finish, Unit: params.unit, PackSize: params.packSize,
			Attrs: params.attrs, Source: params.source, SourceUrl: in.SourceURL,
			ImageUrl: "", ExternalID: "",
		})
		return mapUnique(err, "create item")
	})
	if err != nil {
		return Item{}, err
	}
	return s.loadItem(ctx, s.st.Queries(), wid, workspace.FormatID(created.ID))
}

// UpdateItem replaces the data of a private catalogue item.
func (s *Service) UpdateItem(ctx context.Context, userID, workspaceID, itemID string, in ItemInput) (Item, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Item{}, err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return Item{}, err
	}
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		existing, err := s.visible(ctx, q, wid, iid)
		if err != nil {
			return err
		}
		if !existing.WorkspaceID.Valid {
			return ErrReadOnly
		}
		params, err := s.itemParams(ctx, q, in)
		if err != nil {
			return err
		}
		_, err = q.UpdateCraftCatalogItem(ctx, db.UpdateCraftCatalogItemParams{
			ID: iid, WorkspaceID: wid, CategoryID: params.category.ID, ManufacturerID: params.manufacturerID,
			Article: params.article, Series: in.Series, Name: params.name, ColorName: in.ColorName,
			ColorHex: params.colorHex, ColorSource: params.colorSource, Size: in.Size, Finish: in.Finish,
			Unit: params.unit, PackSize: params.packSize, Attrs: params.attrs,
		})
		return mapUnique(err, "update item")
	})
	if err != nil {
		return Item{}, err
	}
	return s.loadItem(ctx, s.st.Queries(), wid, itemID)
}

// DeleteItem removes a private catalogue item together with its stock.
func (s *Service) DeleteItem(ctx context.Context, userID, workspaceID, itemID string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return err
	}
	return s.st.InTx(ctx, func(q *db.Queries) error {
		existing, err := s.visible(ctx, q, wid, iid)
		if err != nil {
			return err
		}
		if !existing.WorkspaceID.Valid {
			return ErrReadOnly
		}
		if _, err := q.DeleteCraftCatalogItem(ctx, db.DeleteCraftCatalogItemParams{ID: iid, WorkspaceID: wid}); err != nil {
			return fmt.Errorf("delete item: %w", err)
		}
		return nil
	})
}

// ListInventory lists the stock of the workspace.
func (s *Service) ListInventory(ctx context.Context, userID, workspaceID string, f CatalogFilter) ([]StockEntry, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	limit, offset := page(f.Limit, f.Offset)
	var maxQty pgtype.Numeric
	if f.MaxQty != nil {
		maxQty = numeric(*f.MaxQty)
	}
	rows, err := s.st.Queries().ListCraftInventory(ctx, db.ListCraftInventoryParams{
		WorkspaceID: wid, Category: f.Category, Manufacturer: f.Manufacturer,
		Query: strings.TrimSpace(f.Query), MaxQty: maxQty, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}
	out := make([]StockEntry, 0, len(rows))
	for _, r := range rows {
		item := Item{
			ID: workspace.FormatID(r.CatalogItemID), WorkspaceID: workspace.FormatID(r.ItemWorkspaceID),
			Category: r.CategorySlug, CategoryName: r.CategoryName, Manufacturer: deref(r.ManufacturerName),
			Article: r.Article, Series: r.Series, Name: r.Name, ColorName: r.ColorName, ColorHex: r.ColorHex,
			ColorSource: r.ColorSource, Size: r.Size, Finish: r.Finish, Unit: r.Unit, PackSize: numFloatPtr(r.PackSize),
			StockQty: numFloat(r.Qty), InInventory: true,
		}
		out = append(out, StockEntry{
			Item: item, Qty: numFloat(r.Qty), Location: r.Location, Notes: r.Notes, UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

// Adjust changes the stock of an item by delta (negative to use material). The
// stock row is created on first use, so adding an item to the inventory is an
// adjustment from zero. A withdrawal larger than the stock is
// ErrInsufficientStock.
func (s *Service) Adjust(ctx context.Context, userID, workspaceID, itemID string, delta float64, reason string) (StockEntry, error) {
	if delta == 0 || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return StockEntry{}, invalid("Изменение остатка должно быть ненулевым числом.")
	}
	return s.change(ctx, userID, workspaceID, itemID, reason, pgtype.UUID{}, func(ctx context.Context, q *db.Queries, wid, iid pgtype.UUID) (pgtype.Numeric, error) {
		row, err := q.AdjustCraftInventoryQty(ctx, db.AdjustCraftInventoryQtyParams{
			WorkspaceID: wid, CatalogItemID: iid, Qty: numeric(delta),
		})
		if isCheckViolation(err) {
			return pgtype.Numeric{}, ErrInsufficientStock
		}
		if err != nil {
			return pgtype.Numeric{}, fmt.Errorf("adjust stock: %w", err)
		}
		return row.Qty, nil
	}, &delta)
}

// SetQty sets the stock of an item to an absolute value (a stocktake).
func (s *Service) SetQty(ctx context.Context, userID, workspaceID, itemID string, qty float64, reason string) (StockEntry, error) {
	if qty < 0 || math.IsNaN(qty) || math.IsInf(qty, 0) {
		return StockEntry{}, invalid("Остаток не может быть отрицательным.")
	}
	return s.change(ctx, userID, workspaceID, itemID, reason, pgtype.UUID{}, func(ctx context.Context, q *db.Queries, wid, iid pgtype.UUID) (pgtype.Numeric, error) {
		row, err := q.SetCraftInventoryQty(ctx, db.SetCraftInventoryQtyParams{
			WorkspaceID: wid, CatalogItemID: iid, Qty: numeric(qty),
		})
		if err != nil {
			return pgtype.Numeric{}, fmt.Errorf("set stock: %w", err)
		}
		return row.Qty, nil
	}, nil)
}

// RemoveFromInventory drops the stock row of an item (the catalogue item stays).
func (s *Service) RemoveFromInventory(ctx context.Context, userID, workspaceID, itemID string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return err
	}
	rows, err := s.st.Queries().DeleteCraftInventory(ctx, db.DeleteCraftInventoryParams{WorkspaceID: wid, CatalogItemID: iid})
	if err != nil {
		return fmt.Errorf("remove stock: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateStockMeta changes the location and notes of a stock row.
func (s *Service) UpdateStockMeta(ctx context.Context, userID, workspaceID, itemID, location, notes string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return err
	}
	_, err = s.st.Queries().UpdateCraftInventoryMeta(ctx, db.UpdateCraftInventoryMetaParams{
		WorkspaceID: wid, CatalogItemID: iid, Location: location, Notes: notes,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update stock: %w", err)
	}
	return nil
}

// LinkToCatalog re-points a private item to another catalogue item, usually an
// official one found after the private entry was made. The stock moves to the
// target (quantities are summed when the target is already stocked) and the
// private item is deleted.
func (s *Service) LinkToCatalog(ctx context.Context, userID, workspaceID, privateID, targetID string) (StockEntry, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return StockEntry{}, err
	}
	pid, err := parseItem(privateID)
	if err != nil {
		return StockEntry{}, err
	}
	tid, err := parseItem(targetID)
	if err != nil {
		return StockEntry{}, err
	}
	if pid == tid {
		return StockEntry{}, invalid("Позиция уже связана сама с собой.")
	}
	actor, _ := workspace.ParseID(userID)
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		private, err := s.visible(ctx, q, wid, pid)
		if err != nil {
			return err
		}
		if !private.WorkspaceID.Valid {
			return ErrReadOnly
		}
		target, err := s.visible(ctx, q, wid, tid)
		if err != nil {
			return err
		}
		if target.Unit != private.Unit {
			return invalid("Единицы различаются («%s» и «%s»): исправьте единицу у частной позиции перед связыванием.", private.Unit, target.Unit)
		}
		held, err := q.GetCraftInventory(ctx, db.GetCraftInventoryParams{WorkspaceID: wid, CatalogItemID: pid})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get stock: %w", err)
		}
		if err == nil {
			if err := q.RelinkCraftInventory(ctx, db.RelinkCraftInventoryParams{
				WorkspaceID: wid, CatalogItemID: pid, Column3: tid,
			}); err != nil {
				return fmt.Errorf("relink stock: %w", err)
			}
			after, err := q.GetCraftInventory(ctx, db.GetCraftInventoryParams{WorkspaceID: wid, CatalogItemID: tid})
			if err != nil {
				return fmt.Errorf("get linked stock: %w", err)
			}
			if err := q.InsertCraftInventoryLog(ctx, db.InsertCraftInventoryLogParams{
				WorkspaceID: wid, CatalogItemID: tid, UserID: actor, Delta: held.Qty, QtyAfter: after.Qty,
				Reason: "связано с каталогом: " + private.Name,
			}); err != nil {
				return fmt.Errorf("log link: %w", err)
			}
		}
		if _, err := q.DeleteCraftCatalogItem(ctx, db.DeleteCraftCatalogItemParams{ID: pid, WorkspaceID: wid}); err != nil {
			return fmt.Errorf("delete private item: %w", err)
		}
		return nil
	})
	if err != nil {
		return StockEntry{}, err
	}
	return s.stockEntry(ctx, wid, tid)
}

// --- internals -------------------------------------------------------------

func (s *Service) authorize(ctx context.Context, userID, workspaceID string) (pgtype.UUID, error) {
	ws, err := s.workspaces.Require(ctx, userID, workspaceID)
	if errors.Is(err, workspace.ErrNotFound) {
		return pgtype.UUID{}, workspace.ErrNotFound
	}
	if err != nil {
		return pgtype.UUID{}, err
	}
	return workspace.ParseID(ws.ID)
}

func parseItem(id string) (pgtype.UUID, error) {
	u, err := workspace.ParseID(id)
	if err != nil {
		return pgtype.UUID{}, ErrNotFound
	}
	return u, nil
}

// visible loads a catalogue item the workspace may see.
func (s *Service) visible(ctx context.Context, q *db.Queries, wid, iid pgtype.UUID) (db.CraftCatalogItem, error) {
	row, err := q.GetCraftCatalogItem(ctx, db.GetCraftCatalogItemParams{ID: iid, WorkspaceID: wid})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CraftCatalogItem{}, ErrNotFound
	}
	if err != nil {
		return db.CraftCatalogItem{}, fmt.Errorf("get item: %w", err)
	}
	return row, nil
}

func (s *Service) loadItem(ctx context.Context, q *db.Queries, wid pgtype.UUID, itemID string) (Item, error) {
	iid, err := parseItem(itemID)
	if err != nil {
		return Item{}, err
	}
	row, err := s.visible(ctx, q, wid, iid)
	if err != nil {
		return Item{}, err
	}
	category, err := q.GetCraftCategory(ctx, row.CategoryID)
	if err != nil {
		return Item{}, fmt.Errorf("get category: %w", err)
	}
	manufacturer := ""
	if row.ManufacturerID.Valid {
		all, err := q.ListCraftManufacturers(ctx)
		if err != nil {
			return Item{}, fmt.Errorf("list manufacturers: %w", err)
		}
		for _, m := range all {
			if m.ID == row.ManufacturerID {
				manufacturer = m.Name
			}
		}
	}
	item := itemFromRow(row, category.Slug, category.Name, manufacturer)
	if stock, err := q.GetCraftInventory(ctx, db.GetCraftInventoryParams{WorkspaceID: wid, CatalogItemID: iid}); err == nil {
		item.StockQty = numFloat(stock.Qty)
		item.InInventory = true
	}
	return item, nil
}

type itemParamsResult struct {
	category       db.CraftCategory
	manufacturerID pgtype.UUID
	article        string
	name           string
	colorHex       string
	colorSource    string
	unit           string
	packSize       pgtype.Numeric
	attrs          []byte
	source         string
}

// itemParams validates an item input and resolves the category and
// manufacturer.
func (s *Service) itemParams(ctx context.Context, q *db.Queries, in ItemInput) (itemParamsResult, error) {
	var out itemParamsResult
	category, err := q.GetCraftCategoryBySlug(ctx, strings.TrimSpace(in.Category))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, invalid("Неизвестная категория «%s».", in.Category)
	}
	if err != nil {
		return out, fmt.Errorf("get category: %w", err)
	}
	out.category = category

	if m := strings.TrimSpace(in.Manufacturer); m != "" {
		row, err := q.UpsertCraftManufacturer(ctx, db.UpsertCraftManufacturerParams{Name: m})
		if err != nil {
			return out, fmt.Errorf("upsert manufacturer: %w", err)
		}
		out.manufacturerID = row.ID
	}
	out.article = strings.TrimSpace(in.Article)
	out.name = strings.TrimSpace(in.Name)
	if out.name == "" {
		out.name = strings.TrimSpace(strings.Join([]string{in.Manufacturer, in.Article}, " "))
	}
	if out.name == "" {
		return out, invalid("Укажите название или артикул.")
	}
	if utf8.RuneCountInString(out.name) > 200 {
		return out, invalid("Название длиннее 200 символов.")
	}
	if in.ColorHex != "" && !hexColor.MatchString(in.ColorHex) {
		return out, invalid("Цвет должен быть в формате #RRGGBB.")
	}
	out.colorHex = strings.ToUpper(in.ColorHex)
	out.colorSource = in.ColorSource
	if out.colorHex == "" {
		out.colorSource = ""
	} else if out.colorSource == "" {
		out.colorSource = "approx"
	}
	if out.colorSource != "" && out.colorSource != "official" && out.colorSource != "approx" {
		return out, invalid("Источник цвета: official или approx.")
	}
	out.unit = in.Unit
	if out.unit == "" {
		out.unit = category.DefaultUnit
	}
	if !validUnit(out.unit) {
		return out, invalid("Неизвестная единица «%s».", out.unit)
	}
	if in.PackSize != nil {
		if *in.PackSize <= 0 {
			return out, invalid("Размер упаковки должен быть положительным.")
		}
		out.packSize = numeric(*in.PackSize)
	}
	attrs := in.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	if out.attrs, err = json.Marshal(attrs); err != nil {
		return out, invalid("Некорректные атрибуты.")
	}
	out.source = in.Source
	if out.source == "" {
		out.source = "manual"
	}
	return out, nil
}

// change runs one stock mutation: ensure the row, apply fn, log. projectID is
// written to the log row and may be empty.
func (s *Service) change(
	ctx context.Context, userID, workspaceID, itemID, reason string, projectID pgtype.UUID,
	fn func(context.Context, *db.Queries, pgtype.UUID, pgtype.UUID) (pgtype.Numeric, error),
	delta *float64,
) (StockEntry, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return StockEntry{}, err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return StockEntry{}, err
	}
	actor, _ := workspace.ParseID(userID)
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		return s.mutate(ctx, q, wid, iid, actor, projectID, reason, fn, delta)
	})
	if err != nil {
		return StockEntry{}, err
	}
	return s.stockEntry(ctx, wid, iid)
}

// mutate applies one stock mutation inside an existing transaction. The stock
// row is created at zero first; LockCraftInventoryRow then serialises
// concurrent changes of the same row.
func (s *Service) mutate(
	ctx context.Context, q *db.Queries, wid, iid, actor, projectID pgtype.UUID, reason string,
	fn func(context.Context, *db.Queries, pgtype.UUID, pgtype.UUID) (pgtype.Numeric, error),
	delta *float64,
) error {
	if _, err := s.visible(ctx, q, wid, iid); err != nil {
		return err
	}
	// ON CONFLICT DO NOTHING returns no row when the stock row exists.
	if _, err := q.InsertCraftInventory(ctx, db.InsertCraftInventoryParams{
		WorkspaceID: wid, CatalogItemID: iid, Qty: numeric(0),
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("ensure stock row: %w", err)
	}
	before, err := q.LockCraftInventoryRow(ctx, db.LockCraftInventoryRowParams{WorkspaceID: wid, CatalogItemID: iid})
	if err != nil {
		return fmt.Errorf("lock stock row: %w", err)
	}
	after, err := fn(ctx, q, wid, iid)
	if err != nil {
		return err
	}
	d := numFloat(after) - numFloat(before.Qty)
	if delta != nil {
		d = *delta
	}
	return q.InsertCraftInventoryLog(ctx, db.InsertCraftInventoryLogParams{
		WorkspaceID: wid, CatalogItemID: iid, UserID: actor, ProjectID: projectID,
		Delta: numeric(d), QtyAfter: after, Reason: strings.TrimSpace(reason),
	})
}

// adjustInTx applies a relative stock change inside an existing transaction,
// so a feature can combine it with its own writes atomically.
func (s *Service) adjustInTx(
	ctx context.Context, q *db.Queries, wid, iid, actor, projectID pgtype.UUID, delta float64, reason string,
) (pgtype.Numeric, error) {
	var after pgtype.Numeric
	err := s.mutate(ctx, q, wid, iid, actor, projectID, reason,
		func(ctx context.Context, q *db.Queries, wid, iid pgtype.UUID) (pgtype.Numeric, error) {
			row, err := q.AdjustCraftInventoryQty(ctx, db.AdjustCraftInventoryQtyParams{
				WorkspaceID: wid, CatalogItemID: iid, Qty: numeric(delta),
			})
			if isCheckViolation(err) {
				return pgtype.Numeric{}, ErrInsufficientStock
			}
			if err != nil {
				return pgtype.Numeric{}, fmt.Errorf("adjust stock: %w", err)
			}
			after = row.Qty
			return row.Qty, nil
		}, &delta)
	return after, err
}

func (s *Service) stockEntry(ctx context.Context, wid, iid pgtype.UUID) (StockEntry, error) {
	q := s.st.Queries()
	item, err := s.loadItem(ctx, q, wid, workspace.FormatID(iid))
	if err != nil {
		return StockEntry{}, err
	}
	row, err := q.GetCraftInventory(ctx, db.GetCraftInventoryParams{WorkspaceID: wid, CatalogItemID: iid})
	if errors.Is(err, pgx.ErrNoRows) {
		return StockEntry{}, ErrNotFound
	}
	if err != nil {
		return StockEntry{}, fmt.Errorf("get stock: %w", err)
	}
	return StockEntry{Item: item, Qty: numFloat(row.Qty), Location: row.Location, Notes: row.Notes, UpdatedAt: row.UpdatedAt}, nil
}

func validUnit(unit string) bool {
	for _, u := range Units {
		if u == unit {
			return true
		}
	}
	return false
}

func page(limit, offset int) (int32, int32) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return int32(limit), int32(offset)
}

func toCategory(r db.CraftCategory) Category {
	return Category{
		ID: workspace.FormatID(r.ID), Slug: r.Slug, Name: r.Name,
		DefaultUnit: r.DefaultUnit, AttrSchema: json.RawMessage(r.AttrSchema),
	}
}

func itemFromRow(r db.CraftCatalogItem, categorySlug, categoryName, manufacturer string) Item {
	attrs := map[string]any{}
	_ = json.Unmarshal(r.Attrs, &attrs)
	return Item{
		ID: workspace.FormatID(r.ID), WorkspaceID: workspace.FormatID(r.WorkspaceID),
		Category: categorySlug, CategoryName: categoryName, Manufacturer: manufacturer,
		Article: r.Article, Series: r.Series, Name: r.Name, ColorName: r.ColorName,
		ColorHex: r.ColorHex, ColorSource: r.ColorSource, Size: r.Size, Finish: r.Finish,
		Unit: r.Unit, PackSize: numFloatPtr(r.PackSize), Attrs: attrs,
		Source: r.Source, SourceURL: r.SourceUrl, ImageURL: r.ImageUrl,
	}
}

// numeric converts a float to a numeric rounded to the three decimals the
// columns keep.
func numeric(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(v, 'f', 3, 64))
	return n
}

func numFloat(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

func numFloatPtr(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	v := numFloat(n)
	return &v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func isCheckViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23514"
}

// mapUnique turns a duplicate-article violation into a client error.
func mapUnique(err error, what string) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return invalid("Позиция с таким производителем и артикулом уже есть.")
	}
	return fmt.Errorf("%s: %w", what, err)
}
