package craft

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// MCP tool names of the wish list and projects. They are stable: a client
// stores them in its configuration.
const (
	toolWishlistList          = "craft_wishlist_list"
	toolWishlistAdd           = "craft_wishlist_add"
	toolWishlistUpdate        = "craft_wishlist_update"
	toolWishlistRemove        = "craft_wishlist_remove"
	toolWishlistLinkAdd       = "craft_wishlist_link_add"
	toolWishlistLinkRemove    = "craft_wishlist_link_remove"
	toolWishlistMarkBought    = "craft_wishlist_mark_bought"
	toolProjectList           = "craft_project_list"
	toolProjectGet            = "craft_project_get"
	toolProjectCreate         = "craft_project_create"
	toolProjectSetMaterial    = "craft_project_set_material"
	toolProjectUseMaterial    = "craft_project_use_material"
	toolProjectShortageToWish = "craft_project_shortage_to_wishlist"
)

// registerWishlistMCP adds the wish list and project tools to the workspace
// endpoint. It is called from registerMCP.
func (f *Feature) registerWishlistMCP(server *mcpserver.Server) {
	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistList, Title: "List the wish list",
		Description: "List what the workspace wants to buy. By default only entries with status " +
			"wanted are returned; pass ordered or bought for another status, or all for every entry. " +
			"Each entry shows its catalogue item (with the current stock), its links to shops and qty_wanted.",
		Annotations: readAnnotations("List the wish list"),
	}, f.mcpWishlistList)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistAdd, Title: "Add a wish",
		Description: "Add an entry to the wish list: either a catalogue item (catalog_item_id) or free " +
			"text (title) for something that is not in any catalogue yet. The title defaults to the item " +
			"name. qty_wanted is optional and must be positive; priority is 0 (normal), 1 or 2 (high). " +
			"A catalogue item can be wanted only once at a time.",
		Annotations: writeAnnotations("Add a wish", false, false),
	}, f.mcpWishlistAdd)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistUpdate, Title: "Update a wish",
		Description: "Change the quantity, priority, note or status of a wish list entry. Omitted fields " +
			"keep their current values; status is wanted, ordered or bought. Use craft_wishlist_mark_bought " +
			"to also add the purchase to the stock.",
		Annotations: writeAnnotations("Update a wish", false, true),
	}, f.mcpWishlistUpdate)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistRemove, Title: "Remove a wish",
		Description: "Delete a wish list entry together with its links. The stock is not changed.",
		Annotations: writeAnnotations("Remove a wish", true, true),
	}, f.mcpWishlistRemove)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistLinkAdd, Title: "Add a shop link to a wish",
		Description: "Attach a shop page where the wished item can be bought: an http(s) url, the shop " +
			"name, and optionally the price, currency and whether the shop has it in stock.",
		Annotations: writeAnnotations("Add a shop link to a wish", false, false),
	}, f.mcpWishlistLinkAdd)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistLinkRemove, Title: "Remove a shop link from a wish",
		Description: "Delete one shop link of a wish list entry.",
		Annotations: writeAnnotations("Remove a shop link from a wish", true, true),
	}, f.mcpWishlistLinkRemove)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolWishlistMarkBought, Title: "Mark a wish as bought",
		Description: "Mark a wish list entry as bought. With add_to_stock set and a catalogue item, the " +
			"wanted quantity is added to the stock in the same transaction, exactly like craft_stock_adjust; " +
			"a free-text entry is only marked bought. Repeating the call does not add the quantity twice.",
		Annotations: writeAnnotations("Mark a wish as bought", false, true),
	}, f.mcpWishlistMarkBought)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolProjectList, Title: "List projects",
		Description: "List the craft projects of the workspace with the number of materials planned in each.",
		Annotations: readAnnotations("List projects"),
	}, f.mcpProjectList)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolProjectGet, Title: "Get a project",
		Description: "Return one project with its materials: planned quantity, quantity already used, the " +
			"current stock and the shortage. shortage = max(0, qty_planned - qty_used - stock), the quantity " +
			"still to buy.",
		Annotations: readAnnotations("Get a project"),
	}, f.mcpProjectGet)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolProjectCreate, Title: "Create a project",
		Description: "Create a craft project with a name and optional notes. Materials are added with " +
			"craft_project_set_material.",
		Annotations: writeAnnotations("Create a project", false, false),
	}, f.mcpProjectCreate)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolProjectSetMaterial, Title: "Plan a material for a project",
		Description: "Add a catalogue item to the project plan or change its planned quantity. Identify the " +
			"item by item_id or by manufacturer + article. A missing qty_planned clears the plan; used " +
			"quantity and stock are untouched.",
		Annotations: writeAnnotations("Plan a material for a project", false, true),
	}, f.mcpProjectSetMaterial)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolProjectUseMaterial, Title: "Use material for a project",
		Description: "Withdraw material from the stock for a project: decreases the stock and increases the " +
			"used quantity of the material in the same transaction. A withdrawal larger than the stock is an " +
			"error and nothing changes. The inventory log records the project name as the reason.",
		Annotations: writeAnnotations("Use material for a project", false, false),
	}, f.mcpProjectUseMaterial)

	mcpserver.AddWorkspaceTool(server, &mcp.Tool{
		Name: toolProjectShortageToWish, Title: "Add the project shortage to the wish list",
		Description: "Add every material of the project whose shortage is positive (the quantity still to " +
			"buy) to the wish list, skipping materials that are already wanted. Returns the added wishes; " +
			"a repeated call adds nothing.",
		Annotations: writeAnnotations("Add the project shortage to the wish list", false, true),
	}, f.mcpProjectShortageToWish)
}

// --- shapes ----------------------------------------------------------------

type mcpWishLink struct {
	ID        string   `json:"id" jsonschema:"link id (UUID)"`
	URL       string   `json:"url" jsonschema:"shop page, http or https"`
	ShopName  string   `json:"shop_name,omitempty" jsonschema:"shop name"`
	Price     *float64 `json:"price,omitempty" jsonschema:"price when known"`
	Currency  string   `json:"currency,omitempty" jsonschema:"currency of price"`
	InStock   *bool    `json:"in_stock,omitempty" jsonschema:"whether the shop had it in stock at the last check"`
	CreatedAt string   `json:"created_at" jsonschema:"creation time in RFC 3339 UTC"`
}

func wishLinkToMCP(l WishLink) mcpWishLink {
	return mcpWishLink{
		ID: l.ID, URL: l.URL, ShopName: l.ShopName, Price: l.Price, Currency: l.Currency,
		InStock: l.InStock, CreatedAt: l.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

type mcpWish struct {
	ID            string        `json:"id" jsonschema:"wish list entry id (UUID)"`
	CatalogItemID string        `json:"catalog_item_id,omitempty" jsonschema:"catalogue item id, absent for a free-text wish"`
	Title         string        `json:"title" jsonschema:"what is wanted"`
	QtyWanted     *float64      `json:"qty_wanted,omitempty" jsonschema:"how much is wanted, in the item unit"`
	Priority      int           `json:"priority" jsonschema:"0 normal, 1 or 2 high"`
	Note          string        `json:"note,omitempty" jsonschema:"free-text note"`
	Status        string        `json:"status" jsonschema:"wanted, ordered or bought"`
	Item          *mcpItem      `json:"item,omitempty" jsonschema:"catalogue item summary with the current stock, absent for a free-text wish"`
	Links         []mcpWishLink `json:"links" jsonschema:"shop pages for this wish"`
	CreatedAt     string        `json:"created_at" jsonschema:"creation time in RFC 3339 UTC"`
	UpdatedAt     string        `json:"updated_at" jsonschema:"last change time in RFC 3339 UTC"`
}

func wishToMCP(w Wish) mcpWish {
	out := mcpWish{
		ID: w.ID, CatalogItemID: w.CatalogItemID, Title: w.Title, QtyWanted: w.QtyWanted,
		Priority: w.Priority, Note: w.Note, Status: w.Status,
		Links:     make([]mcpWishLink, 0, len(w.Links)),
		CreatedAt: w.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: w.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if w.Item != nil {
		item := itemToMCP(*w.Item)
		out.Item = &item
	}
	for _, link := range w.Links {
		out.Links = append(out.Links, wishLinkToMCP(link))
	}
	return out
}

type mcpWishesOutput struct {
	Wishes []mcpWish `json:"wishes" jsonschema:"wish list entries; empty when there are none"`
}

type mcpWishIDInput struct {
	WishID string `json:"wish_id" jsonschema:"wish list entry id (UUID)"`
}

type mcpWishRemoveOutput struct {
	WishID  string `json:"wish_id" jsonschema:"id of the removed wish list entry"`
	Removed bool   `json:"removed" jsonschema:"always true on success"`
}

type mcpWishLinkRemoveOutput struct {
	WishID  string `json:"wish_id" jsonschema:"wish list entry the link belonged to"`
	LinkID  string `json:"link_id" jsonschema:"id of the removed shop link"`
	Removed bool   `json:"removed" jsonschema:"always true on success"`
}

type mcpWishlistListInput struct {
	Status string `json:"status,omitempty" jsonschema:"wanted (default), ordered, bought or all"`
}

type mcpWishAddInput struct {
	CatalogItemID string   `json:"catalog_item_id,omitempty" jsonschema:"catalogue item id (UUID); omit for a free-text wish"`
	Title         string   `json:"title,omitempty" jsonschema:"what is wanted; defaults to the item name when catalog_item_id is set"`
	QtyWanted     *float64 `json:"qty_wanted,omitempty" jsonschema:"how much is wanted, positive, in the item unit"`
	Priority      int      `json:"priority,omitempty" jsonschema:"0 normal, 1 or 2 high"`
	Note          string   `json:"note,omitempty" jsonschema:"free-text note"`
}

type mcpWishUpdateInput struct {
	WishID    string   `json:"wish_id" jsonschema:"wish list entry id (UUID)"`
	QtyWanted *float64 `json:"qty_wanted,omitempty" jsonschema:"new wanted quantity, positive; omit to keep the current one"`
	Priority  *int     `json:"priority,omitempty" jsonschema:"new priority 0..2; omit to keep the current one"`
	Note      *string  `json:"note,omitempty" jsonschema:"new note; omit to keep the current one"`
	Status    string   `json:"status,omitempty" jsonschema:"wanted, ordered or bought; omit to keep the current status"`
}

type mcpWishLinkAddInput struct {
	WishID   string   `json:"wish_id" jsonschema:"wish list entry id (UUID)"`
	URL      string   `json:"url" jsonschema:"shop page, must start with http:// or https://"`
	ShopName string   `json:"shop_name,omitempty" jsonschema:"shop name"`
	Price    *float64 `json:"price,omitempty" jsonschema:"price, 0 or more"`
	Currency string   `json:"currency,omitempty" jsonschema:"currency of price, for example RUB or EUR"`
	InStock  *bool    `json:"in_stock,omitempty" jsonschema:"whether the shop has it in stock"`
}

type mcpWishLinkRemoveInput struct {
	WishID string `json:"wish_id" jsonschema:"wish list entry id (UUID)"`
	LinkID string `json:"link_id" jsonschema:"shop link id (UUID)"`
}

type mcpWishMarkBoughtInput struct {
	WishID     string `json:"wish_id" jsonschema:"wish list entry id (UUID)"`
	AddToStock bool   `json:"add_to_stock,omitempty" jsonschema:"add the wanted quantity to the stock when the entry points at a catalogue item"`
}

type mcpProjectMaterial struct {
	Item       mcpItem  `json:"item" jsonschema:"the catalogue item with the current stock"`
	QtyPlanned *float64 `json:"qty_planned,omitempty" jsonschema:"planned quantity, absent when no plan is set"`
	QtyUsed    float64  `json:"qty_used" jsonschema:"quantity already used by the project"`
	Shortage   float64  `json:"shortage" jsonschema:"max(0, qty_planned - qty_used - stock): how much is still to buy"`
}

type mcpProject struct {
	ID            string               `json:"id" jsonschema:"project id (UUID)"`
	Name          string               `json:"name" jsonschema:"project name"`
	Notes         string               `json:"notes,omitempty" jsonschema:"free-text notes"`
	MaterialCount int                  `json:"material_count" jsonschema:"number of materials planned"`
	Materials     []mcpProjectMaterial `json:"materials" jsonschema:"materials; empty when the project is fresh or listed"`
	CreatedAt     string               `json:"created_at" jsonschema:"creation time in RFC 3339 UTC"`
}

func projectToMCP(p Project) mcpProject {
	out := mcpProject{
		ID: p.ID, Name: p.Name, Notes: p.Notes, MaterialCount: p.MaterialCount,
		Materials: make([]mcpProjectMaterial, 0, len(p.Materials)),
		CreatedAt: p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	for _, material := range p.Materials {
		out.Materials = append(out.Materials, mcpProjectMaterial{
			Item: itemToMCP(material.Item), QtyPlanned: material.QtyPlanned,
			QtyUsed: material.QtyUsed, Shortage: material.Shortage,
		})
	}
	return out
}

type mcpProjectsOutput struct {
	Projects []mcpProject `json:"projects" jsonschema:"projects of the workspace; empty when there are none"`
}

type mcpProjectIDInput struct {
	ProjectID string `json:"project_id" jsonschema:"project id (UUID)"`
}

type mcpProjectCreateInput struct {
	Name  string `json:"name" jsonschema:"project name, 1 to 200 characters"`
	Notes string `json:"notes,omitempty" jsonschema:"free-text notes"`
}

type mcpProjectSetMaterialInput struct {
	ProjectID string `json:"project_id" jsonschema:"project id (UUID)"`
	mcpItemRef
	QtyPlanned *float64 `json:"qty_planned,omitempty" jsonschema:"planned quantity, 0 or more; omit to clear the plan"`
}

type mcpProjectUseMaterialInput struct {
	ProjectID string `json:"project_id" jsonschema:"project id (UUID)"`
	mcpItemRef
	Qty float64 `json:"qty" jsonschema:"quantity to withdraw from the stock and mark used, positive"`
}

// --- handlers ----------------------------------------------------------------

func (f *Feature) mcpWishlistList(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishlistListInput) (*mcp.CallToolResult, mcpWishesOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftRead)
	if err != nil {
		return nil, mcpWishesOutput{}, err
	}
	wishes, err := f.service.ListWishes(ctx, user, ws, WishFilter{Status: in.Status})
	if err != nil {
		return nil, mcpWishesOutput{}, mcpError(err)
	}
	out := mcpWishesOutput{Wishes: make([]mcpWish, 0, len(wishes))}
	for _, wish := range wishes {
		out.Wishes = append(out.Wishes, wishToMCP(wish))
	}
	return nil, out, nil
}

func (f *Feature) mcpWishlistAdd(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishAddInput) (*mcp.CallToolResult, mcpWish, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWish{}, err
	}
	wish, err := f.service.AddWish(ctx, user, ws, WishInput{
		CatalogItemID: in.CatalogItemID, Title: in.Title, QtyWanted: in.QtyWanted,
		Priority: in.Priority, Note: in.Note,
	})
	if err != nil {
		return nil, mcpWish{}, mcpError(err)
	}
	return nil, wishToMCP(wish), nil
}

func (f *Feature) mcpWishlistUpdate(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishUpdateInput) (*mcp.CallToolResult, mcpWish, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWish{}, err
	}
	current, err := f.service.GetWish(ctx, user, ws, in.WishID)
	if err != nil {
		return nil, mcpWish{}, mcpError(err)
	}
	update := WishUpdate{QtyWanted: current.QtyWanted, Priority: current.Priority, Note: current.Note, Status: current.Status}
	if in.QtyWanted != nil {
		update.QtyWanted = in.QtyWanted
	}
	if in.Priority != nil {
		update.Priority = *in.Priority
	}
	if in.Note != nil {
		update.Note = *in.Note
	}
	if in.Status != "" {
		update.Status = in.Status
	}
	wish, err := f.service.UpdateWish(ctx, user, ws, in.WishID, update)
	if err != nil {
		return nil, mcpWish{}, mcpError(err)
	}
	return nil, wishToMCP(wish), nil
}

func (f *Feature) mcpWishlistRemove(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishIDInput) (*mcp.CallToolResult, mcpWishRemoveOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWishRemoveOutput{}, err
	}
	if err := f.service.DeleteWish(ctx, user, ws, in.WishID); err != nil {
		return nil, mcpWishRemoveOutput{}, mcpError(err)
	}
	return nil, mcpWishRemoveOutput{WishID: in.WishID, Removed: true}, nil
}

func (f *Feature) mcpWishlistLinkAdd(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishLinkAddInput) (*mcp.CallToolResult, mcpWishLink, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWishLink{}, err
	}
	link, err := f.service.AddWishLink(ctx, user, ws, in.WishID, WishLinkInput{
		URL: in.URL, ShopName: in.ShopName, Price: in.Price, Currency: in.Currency, InStock: in.InStock,
	})
	if err != nil {
		return nil, mcpWishLink{}, mcpError(err)
	}
	return nil, wishLinkToMCP(link), nil
}

func (f *Feature) mcpWishlistLinkRemove(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishLinkRemoveInput) (*mcp.CallToolResult, mcpWishLinkRemoveOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWishLinkRemoveOutput{}, err
	}
	if err := f.service.RemoveWishLink(ctx, user, ws, in.WishID, in.LinkID); err != nil {
		return nil, mcpWishLinkRemoveOutput{}, mcpError(err)
	}
	return nil, mcpWishLinkRemoveOutput{WishID: in.WishID, LinkID: in.LinkID, Removed: true}, nil
}

func (f *Feature) mcpWishlistMarkBought(ctx context.Context, _ *mcp.CallToolRequest, in mcpWishMarkBoughtInput) (*mcp.CallToolResult, mcpWish, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWish{}, err
	}
	wish, err := f.service.MarkBought(ctx, user, ws, in.WishID, in.AddToStock)
	if err != nil {
		return nil, mcpWish{}, mcpError(err)
	}
	return nil, wishToMCP(wish), nil
}

func (f *Feature) mcpProjectList(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpProjectsOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftRead)
	if err != nil {
		return nil, mcpProjectsOutput{}, err
	}
	projects, err := f.service.ListProjects(ctx, user, ws)
	if err != nil {
		return nil, mcpProjectsOutput{}, mcpError(err)
	}
	out := mcpProjectsOutput{Projects: make([]mcpProject, 0, len(projects))}
	for _, project := range projects {
		out.Projects = append(out.Projects, projectToMCP(project))
	}
	return nil, out, nil
}

func (f *Feature) mcpProjectGet(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectIDInput) (*mcp.CallToolResult, mcpProject, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftRead)
	if err != nil {
		return nil, mcpProject{}, err
	}
	project, err := f.service.GetProject(ctx, user, ws, in.ProjectID)
	if err != nil {
		return nil, mcpProject{}, mcpError(err)
	}
	return nil, projectToMCP(project), nil
}

func (f *Feature) mcpProjectCreate(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectCreateInput) (*mcp.CallToolResult, mcpProject, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpProject{}, err
	}
	project, err := f.service.CreateProject(ctx, user, ws, ProjectInput{Name: in.Name, Notes: in.Notes})
	if err != nil {
		return nil, mcpProject{}, mcpError(err)
	}
	return nil, projectToMCP(project), nil
}

func (f *Feature) mcpProjectSetMaterial(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectSetMaterialInput) (*mcp.CallToolResult, mcpProject, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpProject{}, err
	}
	id, err := f.resolve(ctx, user, ws, in.mcpItemRef)
	if err != nil {
		return nil, mcpProject{}, err
	}
	project, err := f.service.SetProjectMaterial(ctx, user, ws, in.ProjectID, id, in.QtyPlanned)
	if err != nil {
		return nil, mcpProject{}, mcpError(err)
	}
	return nil, projectToMCP(project), nil
}

func (f *Feature) mcpProjectUseMaterial(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectUseMaterialInput) (*mcp.CallToolResult, mcpProject, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpProject{}, err
	}
	id, err := f.resolve(ctx, user, ws, in.mcpItemRef)
	if err != nil {
		return nil, mcpProject{}, err
	}
	project, err := f.service.UseMaterial(ctx, user, ws, in.ProjectID, id, in.Qty)
	if err != nil {
		return nil, mcpProject{}, mcpError(err)
	}
	return nil, projectToMCP(project), nil
}

func (f *Feature) mcpProjectShortageToWish(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectIDInput) (*mcp.CallToolResult, mcpWishesOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite)
	if err != nil {
		return nil, mcpWishesOutput{}, err
	}
	added, err := f.service.ShortageToWishlist(ctx, user, ws, in.ProjectID)
	if err != nil {
		return nil, mcpWishesOutput{}, mcpError(err)
	}
	out := mcpWishesOutput{Wishes: make([]mcpWish, 0, len(added))}
	for _, wish := range added {
		out.Wishes = append(out.Wishes, wishToMCP(wish))
	}
	return nil, out, nil
}
