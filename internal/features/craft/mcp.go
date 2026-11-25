package craft

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// MCP tool names. They are stable: a client stores them in its configuration.
const (
	toolReference   = "craft_reference"
	toolSearch      = "craft_catalog_search"
	toolItemCreate  = "craft_item_create"
	toolItemUpdate  = "craft_item_update"
	toolItemDelete  = "craft_item_delete"
	toolItemLink    = "craft_item_link"
	toolInventory   = "craft_inventory_list"
	toolStockAdjust = "craft_stock_adjust"
	toolStockSet    = "craft_stock_set"
	toolStockRemove = "craft_stock_remove"
)

func boolPtr(v bool) *bool { return &v }

// readAnnotations marks a tool that only reads data.
func readAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// writeAnnotations marks a tool that changes data; destructive also means it can
// remove something.
func writeAnnotations(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(destructive), IdempotentHint: idempotent, OpenWorldHint: boolPtr(false)}
}

// registerMCP adds the warehouse tools. Each takes an optional space argument
// naming the warehouse it works with.
func (f *Feature) registerMCP(server *mcpserver.Server) {
	f.registerReferenceMCP(server)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolSearch, Title: "Search the catalogue",
		Description: "Search the shared catalogue (manufacturer catalogues such as PRECIOSA or DMC) " +
			"and the private items of this workspace. Filters are optional; query matches an " +
			"article prefix or a part of the name or colour. Each result shows how much of it " +
			"the workspace holds (stock_qty) and its unit.",
		Annotations: readAnnotations("Search the catalogue"),
	}, f.mcpSearch)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolInventory, Title: "List stock",
		Description: "List what the workspace holds: items with quantity and unit, optionally " +
			"filtered by category, manufacturer or text. Use max_qty to find items that are " +
			"running low.",
		Annotations: readAnnotations("List stock"),
	}, f.mcpInventory)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolStockAdjust, Title: "Change stock by a delta",
		Description: "Add to or use up material: delta is positive to add and negative to use. " +
			"Identify the item by item_id, or by manufacturer + article (+ category when ambiguous). " +
			"The stock row is created on first use. A withdrawal larger than the stock is an error. " +
			"The change is logged with the optional reason.",
		Annotations: writeAnnotations("Change stock by a delta", false, false),
	}, f.mcpStockAdjust)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolStockSet, Title: "Set stock to a value",
		Description: "Set the quantity of an item to an absolute value (a stocktake). Identify the " +
			"item like craft_stock_adjust. The change is logged.",
		Annotations: writeAnnotations("Set stock to a value", false, true),
	}, f.mcpStockSet)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolStockRemove, Title: "Remove an item from stock",
		Description: "Delete the stock row of an item (the catalogue item itself stays).",
		Annotations: writeAnnotations("Remove an item from stock", true, true),
	}, f.mcpStockRemove)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolItemCreate, Title: "Create a private item",
		Description: "Create a catalogue item private to this workspace, for material that is in no " +
			"manufacturer catalogue (for example a thread of Mallorca pearls). The unit defaults " +
			"to the category's default unit. It can be linked to an official catalogue item later " +
			"with craft_item_link.",
		Annotations: writeAnnotations("Create a private item", false, false),
	}, f.mcpItemCreate)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolItemUpdate, Title: "Update a private item",
		Description: "Replace the data of a private item of this workspace. All fields are replaced, " +
			"so read the item with craft_catalog_search first and send the full data. Global " +
			"catalogue items cannot be changed.",
		Annotations: writeAnnotations("Update a private item", false, true),
	}, f.mcpItemUpdate)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolItemDelete, Title: "Delete a private item",
		Description: "Delete a private item together with its stock. Global catalogue items cannot be deleted.",
		Annotations: writeAnnotations("Delete a private item", true, true),
	}, f.mcpItemDelete)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolItemLink, Title: "Link a private item to the catalogue",
		Description: "Move the stock of a private item to another catalogue item (usually the official " +
			"one) and delete the private item. Units must match; quantities are summed when the " +
			"target is already in stock.",
		Annotations: writeAnnotations("Link a private item to the catalogue", true, false),
	}, f.mcpItemLink)

	f.registerWishlistMCP(server)
}

// --- shapes ----------------------------------------------------------------

type mcpItem struct {
	ID           string         `json:"id" jsonschema:"catalogue item id (UUID)"`
	Private      bool           `json:"private" jsonschema:"true for an item private to this workspace, false for a global catalogue item"`
	Category     string         `json:"category" jsonschema:"category slug"`
	CategoryName string         `json:"category_name" jsonschema:"category name in Russian"`
	Manufacturer string         `json:"manufacturer,omitempty" jsonschema:"manufacturer name"`
	Article      string         `json:"article,omitempty" jsonschema:"manufacturer article number"`
	Series       string         `json:"series,omitempty" jsonschema:"series or variety"`
	Name         string         `json:"name" jsonschema:"item name"`
	ColorName    string         `json:"color_name,omitempty" jsonschema:"colour name"`
	ColorHex     string         `json:"color_hex,omitempty" jsonschema:"colour as #RRGGBB"`
	ColorSource  string         `json:"color_source,omitempty" jsonschema:"official or approx: how trustworthy color_hex is"`
	Size         string         `json:"size,omitempty" jsonschema:"size, free text"`
	Finish       string         `json:"finish,omitempty" jsonschema:"finish or coating, free text"`
	Unit         string         `json:"unit" jsonschema:"unit the quantity is counted in"`
	PackSize     *float64       `json:"pack_size,omitempty" jsonschema:"how many base units one pack holds, when known"`
	Attrs        map[string]any `json:"attrs,omitempty" jsonschema:"category specific attributes"`
	ImageURL     string         `json:"image_url,omitempty" jsonschema:"link to a photo of the item"`
	StockQty     float64        `json:"stock_qty" jsonschema:"quantity this workspace holds, in unit"`
	InInventory  bool           `json:"in_inventory" jsonschema:"whether the workspace has a stock row for the item"`
}

func itemToMCP(i Item) mcpItem {
	return mcpItem{
		ID: i.ID, Private: i.Private(), Category: i.Category, CategoryName: i.CategoryName,
		Manufacturer: i.Manufacturer, Article: i.Article, Series: i.Series, Name: i.Name,
		ColorName: i.ColorName, ColorHex: i.ColorHex, ColorSource: i.ColorSource, Size: i.Size,
		Finish: i.Finish, Unit: i.Unit, PackSize: i.PackSize, Attrs: i.Attrs, ImageURL: i.ImageURL,
		StockQty: i.StockQty, InInventory: i.InInventory,
	}
}

type mcpStock struct {
	Item      mcpItem `json:"item" jsonschema:"the catalogue item"`
	Qty       float64 `json:"qty" jsonschema:"quantity held, in item.unit"`
	Location  string  `json:"location,omitempty" jsonschema:"where it is stored"`
	Notes     string  `json:"notes,omitempty" jsonschema:"notes about this stock"`
	UpdatedAt string  `json:"updated_at" jsonschema:"last change in RFC 3339 UTC"`
}

func stockToMCP(e StockEntry) mcpStock {
	item := itemToMCP(e.Item)
	item.StockQty, item.InInventory = e.Qty, true
	return mcpStock{Item: item, Qty: e.Qty, Location: e.Location, Notes: e.Notes, UpdatedAt: e.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")}
}

// mcpSpace names the warehouse of a call.
type mcpSpace struct {
	Space string `json:"space,omitempty" jsonschema:"warehouse space id or name (see spaces_list); may be omitted when the user has exactly one"`
}

type mcpFilter struct {
	mcpSpace
	Category     string `json:"category,omitempty" jsonschema:"category slug or name, see craft_reference"`
	Manufacturer string `json:"manufacturer,omitempty" jsonschema:"manufacturer name, case-insensitive exact match"`
	Query        string `json:"query,omitempty" jsonschema:"article prefix or part of the name or colour name"`
	Limit        int    `json:"limit,omitempty" jsonschema:"page size 1..200, default 50"`
	Offset       int    `json:"offset,omitempty" jsonschema:"number of results to skip"`
}

type mcpSearchInput struct {
	mcpFilter
	OnlyInStock bool `json:"only_in_stock,omitempty" jsonschema:"only items the workspace holds"`
}

type mcpItemsOutput struct {
	Items []mcpItem `json:"items" jsonschema:"matching items"`
}

type mcpInventoryInput struct {
	mcpFilter
	MaxQty *float64 `json:"max_qty,omitempty" jsonschema:"only rows with quantity at most this value, to find low stock"`
}

type mcpInventoryOutput struct {
	Stock []mcpStock `json:"stock" jsonschema:"stock rows"`
}

// mcpItemRef identifies an item by id or by manufacturer + article.
type mcpItemRef struct {
	mcpSpace
	ItemID       string `json:"item_id,omitempty" jsonschema:"catalogue item id (UUID); use it when known"`
	Manufacturer string `json:"manufacturer,omitempty" jsonschema:"manufacturer name, with article, instead of item_id"`
	Article      string `json:"article,omitempty" jsonschema:"article number, with manufacturer, instead of item_id"`
	Category     string `json:"category,omitempty" jsonschema:"category slug, only to disambiguate manufacturer + article"`
}

type mcpAdjustInput struct {
	mcpItemRef
	Delta  float64 `json:"delta" jsonschema:"change of the quantity in the item's unit: positive adds, negative uses up"`
	Reason string  `json:"reason,omitempty" jsonschema:"why, for example the project name"`
}

type mcpSetInput struct {
	mcpItemRef
	Qty    float64 `json:"qty" jsonschema:"new absolute quantity in the item's unit, 0 or more"`
	Reason string  `json:"reason,omitempty" jsonschema:"why, for example stocktake"`
}

type mcpRemoveOutput struct {
	ItemID  string `json:"item_id" jsonschema:"item whose stock row was removed"`
	Removed bool   `json:"removed" jsonschema:"always true on success"`
}

type mcpItemData struct {
	Category     string         `json:"category" jsonschema:"category slug or name, see craft_reference"`
	Manufacturer string         `json:"manufacturer,omitempty" jsonschema:"manufacturer name; created in the warehouse when unknown"`
	Article      string         `json:"article,omitempty" jsonschema:"article number"`
	Series       string         `json:"series,omitempty" jsonschema:"series or variety"`
	Name         string         `json:"name,omitempty" jsonschema:"item name; defaults to manufacturer + article"`
	ColorName    string         `json:"color_name,omitempty" jsonschema:"colour name"`
	ColorHex     string         `json:"color_hex,omitempty" jsonschema:"colour as #RRGGBB"`
	ColorSource  string         `json:"color_source,omitempty" jsonschema:"official or approx; approx when omitted and color_hex is set"`
	Size         string         `json:"size,omitempty" jsonschema:"size, free text"`
	Finish       string         `json:"finish,omitempty" jsonschema:"finish or coating, free text"`
	Unit         string         `json:"unit,omitempty" jsonschema:"unit; the category default when omitted"`
	PackSize     *float64       `json:"pack_size,omitempty" jsonschema:"how many base units one pack holds"`
	Attrs        map[string]any `json:"attrs,omitempty" jsonschema:"values of the category's extra fields by key, see craft_reference"`
	ImageURL     string         `json:"image_url,omitempty" jsonschema:"http(s) link to a photo of the item"`
}

func (d mcpItemData) input() ItemInput {
	return ItemInput{
		Category: d.Category, Manufacturer: d.Manufacturer, Article: d.Article, Series: d.Series, Name: d.Name,
		ColorName: d.ColorName, ColorHex: d.ColorHex, ColorSource: d.ColorSource, Size: d.Size, Finish: d.Finish,
		Unit: d.Unit, PackSize: d.PackSize, Attrs: d.Attrs, ImageURL: d.ImageURL,
	}
}

type mcpItemCreateInput struct {
	mcpSpace
	mcpItemData
}

type mcpItemUpdateInput struct {
	mcpSpace
	ItemID string `json:"item_id" jsonschema:"id of the private item to replace"`
	mcpItemData
}

type mcpItemIDInput struct {
	mcpSpace
	ItemID string `json:"item_id" jsonschema:"catalogue item id (UUID)"`
}

type mcpLinkInput struct {
	mcpSpace
	PrivateItemID string `json:"private_item_id" jsonschema:"the private item to merge away"`
	TargetItemID  string `json:"target_item_id" jsonschema:"the catalogue item that receives its stock"`
}

// --- handlers ----------------------------------------------------------------

// access authorises one call: the token scope, and the warehouse the call
// names among those the user is a member of.
func (f *Feature) access(ctx context.Context, scope, space string) (userID, workspaceID string, err error) {
	principal, ok := mcpserver.PrincipalFromContext(ctx)
	if !ok {
		return "", "", mcpserver.ErrUnauthenticated
	}
	if err := mcpserver.RequireScope(principal, scope); err != nil {
		return "", "", err
	}
	resolved, err := f.service.ResolveSpace(ctx, principal.ID, space)
	if err != nil {
		return "", "", mcpError(err)
	}
	return principal.ID, resolved, nil
}

func (f *Feature) mcpSearch(ctx context.Context, _ *mcp.CallToolRequest, in mcpSearchInput) (*mcp.CallToolResult, mcpItemsOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftRead, in.Space)
	if err != nil {
		return nil, mcpItemsOutput{}, err
	}
	items, err := f.service.SearchCatalog(ctx, user, ws, CatalogFilter{
		Category: in.Category, Manufacturer: in.Manufacturer, Query: in.Query,
		OnlyInStock: in.OnlyInStock, Limit: in.Limit, Offset: in.Offset,
	})
	if err != nil {
		return nil, mcpItemsOutput{}, mcpError(err)
	}
	out := mcpItemsOutput{Items: make([]mcpItem, 0, len(items))}
	for _, i := range items {
		out.Items = append(out.Items, itemToMCP(i))
	}
	return nil, out, nil
}

func (f *Feature) mcpInventory(ctx context.Context, _ *mcp.CallToolRequest, in mcpInventoryInput) (*mcp.CallToolResult, mcpInventoryOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftRead, in.Space)
	if err != nil {
		return nil, mcpInventoryOutput{}, err
	}
	rows, err := f.service.ListInventory(ctx, user, ws, CatalogFilter{
		Category: in.Category, Manufacturer: in.Manufacturer, Query: in.Query,
		MaxQty: in.MaxQty, Limit: in.Limit, Offset: in.Offset,
	})
	if err != nil {
		return nil, mcpInventoryOutput{}, mcpError(err)
	}
	out := mcpInventoryOutput{Stock: make([]mcpStock, 0, len(rows))}
	for _, r := range rows {
		out.Stock = append(out.Stock, stockToMCP(r))
	}
	return nil, out, nil
}

// resolve finds the item a tool call names: by id, or by manufacturer + article
// (+ category). An ambiguous or unknown reference is an error that lists the
// candidates, so the model can retry with the id.
func (f *Feature) resolve(ctx context.Context, user, ws string, ref mcpItemRef) (string, error) {
	if ref.ItemID != "" {
		return ref.ItemID, nil
	}
	if ref.Article == "" {
		return "", errors.New("name the item: item_id, or manufacturer and article")
	}
	found, err := f.service.SearchCatalog(ctx, user, ws, CatalogFilter{
		Category: ref.Category, Manufacturer: ref.Manufacturer, Query: ref.Article, Limit: MaxLimit,
	})
	if err != nil {
		return "", mcpError(err)
	}
	var exact []Item
	for _, i := range found {
		if strings.EqualFold(i.Article, ref.Article) {
			exact = append(exact, i)
		}
	}
	switch len(exact) {
	case 1:
		return exact[0].ID, nil
	case 0:
		return "", fmt.Errorf("no catalogue item with article %q (manufacturer %q); search the catalogue or create a private item", ref.Article, ref.Manufacturer)
	}
	var lines []string
	for _, i := range exact {
		lines = append(lines, fmt.Sprintf("%s (%s %s, category %s)", i.ID, i.Manufacturer, i.Name, i.Category))
	}
	return "", fmt.Errorf("article %q is ambiguous, pass item_id or category: %s", ref.Article, strings.Join(lines, "; "))
}

func (f *Feature) mcpStockAdjust(ctx context.Context, _ *mcp.CallToolRequest, in mcpAdjustInput) (*mcp.CallToolResult, mcpStock, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpStock{}, err
	}
	id, err := f.resolve(ctx, user, ws, in.mcpItemRef)
	if err != nil {
		return nil, mcpStock{}, err
	}
	entry, err := f.service.Adjust(ctx, user, ws, id, in.Delta, in.Reason)
	if err != nil {
		return nil, mcpStock{}, mcpError(err)
	}
	return nil, stockToMCP(entry), nil
}

func (f *Feature) mcpStockSet(ctx context.Context, _ *mcp.CallToolRequest, in mcpSetInput) (*mcp.CallToolResult, mcpStock, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpStock{}, err
	}
	id, err := f.resolve(ctx, user, ws, in.mcpItemRef)
	if err != nil {
		return nil, mcpStock{}, err
	}
	entry, err := f.service.SetQty(ctx, user, ws, id, in.Qty, in.Reason)
	if err != nil {
		return nil, mcpStock{}, mcpError(err)
	}
	return nil, stockToMCP(entry), nil
}

func (f *Feature) mcpStockRemove(ctx context.Context, _ *mcp.CallToolRequest, in mcpItemRef) (*mcp.CallToolResult, mcpRemoveOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpRemoveOutput{}, err
	}
	id, err := f.resolve(ctx, user, ws, in)
	if err != nil {
		return nil, mcpRemoveOutput{}, err
	}
	if err := f.service.RemoveFromInventory(ctx, user, ws, id); err != nil {
		return nil, mcpRemoveOutput{}, mcpError(err)
	}
	return nil, mcpRemoveOutput{ItemID: id, Removed: true}, nil
}

func (f *Feature) mcpItemCreate(ctx context.Context, _ *mcp.CallToolRequest, in mcpItemCreateInput) (*mcp.CallToolResult, mcpItem, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpItem{}, err
	}
	item, err := f.service.CreateItem(ctx, user, ws, in.input())
	if err != nil {
		return nil, mcpItem{}, mcpError(err)
	}
	return nil, itemToMCP(item), nil
}

func (f *Feature) mcpItemUpdate(ctx context.Context, _ *mcp.CallToolRequest, in mcpItemUpdateInput) (*mcp.CallToolResult, mcpItem, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpItem{}, err
	}
	item, err := f.service.UpdateItem(ctx, user, ws, in.ItemID, in.input())
	if err != nil {
		return nil, mcpItem{}, mcpError(err)
	}
	return nil, itemToMCP(item), nil
}

func (f *Feature) mcpItemDelete(ctx context.Context, _ *mcp.CallToolRequest, in mcpItemIDInput) (*mcp.CallToolResult, mcpRemoveOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpRemoveOutput{}, err
	}
	if err := f.service.DeleteItem(ctx, user, ws, in.ItemID); err != nil {
		return nil, mcpRemoveOutput{}, mcpError(err)
	}
	return nil, mcpRemoveOutput{ItemID: in.ItemID, Removed: true}, nil
}

func (f *Feature) mcpItemLink(ctx context.Context, _ *mcp.CallToolRequest, in mcpLinkInput) (*mcp.CallToolResult, mcpStock, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpStock{}, err
	}
	entry, err := f.service.LinkToCatalog(ctx, user, ws, in.PrivateItemID, in.TargetItemID)
	if err != nil {
		return nil, mcpStock{}, mcpError(err)
	}
	return nil, stockToMCP(entry), nil
}

// mcpError turns a service error into a message the model can act on.
func mcpError(err error) error {
	var invalid *ValidationError
	switch {
	case errors.As(err, &invalid):
		return errors.New(invalid.Message)
	case errors.Is(err, ErrNotFound):
		return errors.New("not found: the object does not exist or is not visible to this workspace")
	case errors.Is(err, ErrReadOnly):
		return errors.New("global catalogue items are read-only; create a private item instead")
	case errors.Is(err, ErrInsufficientStock):
		return errors.New("not enough stock for this withdrawal")
	case errors.Is(err, workspace.ErrNotFound):
		return errors.New("workspace not found")
	default:
		return errors.New("craft operation failed: " + err.Error())
	}
}
