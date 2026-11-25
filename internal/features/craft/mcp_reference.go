package craft

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// registerReferenceMCP adds the tools that describe and configure a warehouse.
func (f *Feature) registerReferenceMCP(server *mcpserver.Server) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: toolReference, Title: "Warehouse reference data",
		Description: "List how a warehouse is configured: material categories (with their default " +
			"unit and extra fields), manufacturers, units and the official catalogues. Call it " +
			"before searching or creating items to learn the valid categories, units and fields.",
		Annotations: readAnnotations("Warehouse reference data"),
	}, f.mcpReference)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_category_save", Title: "Create or change a category",
		Description: "Create a category (omit id) or change one. default_unit must be a unit of the " +
			"warehouse. fields defines extra values items of the category carry in attrs; on a " +
			"change, omitted name, default_unit or fields keep their current values.",
		Annotations: writeAnnotations("Create or change a category", false, false),
	}, f.mcpCategorySave)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_category_delete", Title: "Delete a category",
		Description: "Delete a category that none of the warehouse's own items use.",
		Annotations: writeAnnotations("Delete a category", true, true),
	}, f.mcpCategoryDelete)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_manufacturer_save", Title: "Create or change a manufacturer",
		Description: "Create an own manufacturer (omit id) or change one. Manufacturers of the " +
			"official catalogues are read-only. A change replaces both name and website.",
		Annotations: writeAnnotations("Create or change a manufacturer", false, false),
	}, f.mcpManufacturerSave)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_manufacturer_delete", Title: "Delete a manufacturer",
		Description: "Delete an own manufacturer that no item uses.",
		Annotations: writeAnnotations("Delete a manufacturer", true, true),
	}, f.mcpManufacturerDelete)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_unit_save", Title: "Create or rename a unit",
		Description: "Create a unit (omit id) or rename one. A rename follows into own items and " +
			"categories counted in it; quantities are not converted.",
		Annotations: writeAnnotations("Create or rename a unit", false, false),
	}, f.mcpUnitSave)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_unit_delete", Title: "Delete a unit",
		Description: "Delete a unit that no own item and no category uses.",
		Annotations: writeAnnotations("Delete a unit", true, true),
	}, f.mcpUnitDelete)
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "craft_catalogs_set", Title: "Connect official catalogues",
		Description: "Replace the set of official catalogues the warehouse searches (see " +
			"craft_reference for the available sources). Stock already held stays when a " +
			"catalogue is disconnected.",
		Annotations: writeAnnotations("Connect official catalogues", false, true),
	}, f.mcpCatalogsSet)
}

type mcpField struct {
	Key     string   `json:"key,omitempty" jsonschema:"name of the value in item attrs; omit for a new field"`
	Label   string   `json:"label" jsonschema:"label shown to people"`
	Type    string   `json:"type" jsonschema:"text, number, bool or select"`
	Options []string `json:"options,omitempty" jsonschema:"allowed values of a select field"`
}

type mcpCategory struct {
	ID          string     `json:"id" jsonschema:"category id"`
	Slug        string     `json:"slug" jsonschema:"category slug to use in filters and item data (a name works too)"`
	Name        string     `json:"name" jsonschema:"category name"`
	DefaultUnit string     `json:"default_unit" jsonschema:"unit used when an item does not name one"`
	Fields      []mcpField `json:"fields" jsonschema:"extra fields; their values go to item attrs under key"`
	OwnItems    int        `json:"own_items" jsonschema:"how many of the warehouse's own items use it"`
}

type mcpManufacturer struct {
	ID       string `json:"id" jsonschema:"manufacturer id"`
	Name     string `json:"name" jsonschema:"manufacturer name"`
	Website  string `json:"website,omitempty" jsonschema:"website"`
	Official bool   `json:"official" jsonschema:"from an official catalogue, read-only"`
}

type mcpUnit struct {
	ID   string `json:"id" jsonschema:"unit id"`
	Name string `json:"name" jsonschema:"unit name"`
}

type mcpCatalogSource struct {
	Source  string `json:"source" jsonschema:"catalogue key for craft_catalogs_set"`
	Name    string `json:"name" jsonschema:"catalogue name"`
	Enabled bool   `json:"enabled" jsonschema:"connected to this warehouse"`
	Items   int    `json:"items" jsonschema:"number of positions"`
}

type mcpReferenceOutput struct {
	Categories    []mcpCategory      `json:"categories" jsonschema:"categories of the warehouse"`
	Manufacturers []mcpManufacturer  `json:"manufacturers" jsonschema:"own and connected official manufacturers"`
	Units         []mcpUnit          `json:"units" jsonschema:"units of the warehouse"`
	Catalogs      []mcpCatalogSource `json:"catalogs" jsonschema:"official catalogues"`
}

type mcpCategorySaveInput struct {
	mcpSpace
	ID          string      `json:"id,omitempty" jsonschema:"category to change; omit to create"`
	Name        string      `json:"name,omitempty" jsonschema:"category name"`
	DefaultUnit string      `json:"default_unit,omitempty" jsonschema:"a unit of the warehouse"`
	Fields      *[]mcpField `json:"fields,omitempty" jsonschema:"all extra fields of the category; replaces the current list"`
}

type mcpManufacturerSaveInput struct {
	mcpSpace
	ID      string `json:"id,omitempty" jsonschema:"manufacturer to change; omit to create"`
	Name    string `json:"name" jsonschema:"manufacturer name"`
	Website string `json:"website,omitempty" jsonschema:"http(s) website"`
}

type mcpUnitSaveInput struct {
	mcpSpace
	ID   string `json:"id,omitempty" jsonschema:"unit to rename; omit to create"`
	Name string `json:"name" jsonschema:"unit name, up to 24 characters"`
}

type mcpReferenceIDInput struct {
	mcpSpace
	ID string `json:"id" jsonschema:"id from craft_reference"`
}

type mcpDeletedOutput struct {
	ID      string `json:"id" jsonschema:"deleted id"`
	Deleted bool   `json:"deleted" jsonschema:"always true on success"`
}

type mcpCatalogsSetInput struct {
	mcpSpace
	Sources []string `json:"sources" jsonschema:"catalogue keys to keep connected; empty disconnects all"`
}

type mcpCatalogsOutput struct {
	Catalogs []mcpCatalogSource `json:"catalogs" jsonschema:"official catalogues"`
}

func categoryToMCP(c Category) mcpCategory {
	out := mcpCategory{ID: c.ID, Slug: c.Slug, Name: c.Name, DefaultUnit: c.DefaultUnit, Fields: []mcpField{}, OwnItems: c.Items}
	for _, field := range c.Fields {
		out.Fields = append(out.Fields, mcpField(field))
	}
	return out
}

func catalogsToMCP(sources []CatalogSource) []mcpCatalogSource {
	out := make([]mcpCatalogSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, mcpCatalogSource(source))
	}
	return out
}

func (f *Feature) mcpReference(ctx context.Context, _ *mcp.CallToolRequest, in mcpSpace) (*mcp.CallToolResult, mcpReferenceOutput, error) {
	out := mcpReferenceOutput{Categories: []mcpCategory{}, Manufacturers: []mcpManufacturer{}, Units: []mcpUnit{}}
	user, ws, err := f.access(ctx, auth.ScopeCraftRead, in.Space)
	if err != nil {
		return nil, out, err
	}
	categories, err := f.service.Categories(ctx, user, ws)
	if err != nil {
		return nil, out, mcpError(err)
	}
	for _, category := range categories {
		out.Categories = append(out.Categories, categoryToMCP(category))
	}
	manufacturers, err := f.service.Manufacturers(ctx, user, ws)
	if err != nil {
		return nil, out, mcpError(err)
	}
	for _, m := range manufacturers {
		out.Manufacturers = append(out.Manufacturers, mcpManufacturer{ID: m.ID, Name: m.Name, Website: m.Website, Official: m.Official})
	}
	units, err := f.service.ListUnits(ctx, user, ws)
	if err != nil {
		return nil, out, mcpError(err)
	}
	for _, unit := range units {
		out.Units = append(out.Units, mcpUnit(unit))
	}
	sources, err := f.service.CatalogSources(ctx, user, ws)
	if err != nil {
		return nil, out, mcpError(err)
	}
	out.Catalogs = catalogsToMCP(sources)
	return nil, out, nil
}

func (f *Feature) mcpCategorySave(ctx context.Context, _ *mcp.CallToolRequest, in mcpCategorySaveInput) (*mcp.CallToolResult, mcpCategory, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpCategory{}, err
	}
	input := CategoryInput{Name: in.Name, DefaultUnit: in.DefaultUnit}
	if in.ID != "" {
		categories, err := f.service.Categories(ctx, user, ws)
		if err != nil {
			return nil, mcpCategory{}, mcpError(err)
		}
		for _, current := range categories {
			if current.ID != in.ID {
				continue
			}
			if input.Name == "" {
				input.Name = current.Name
			}
			if input.DefaultUnit == "" {
				input.DefaultUnit = current.DefaultUnit
			}
			input.Fields = current.Fields
		}
	}
	if in.Fields != nil {
		input.Fields = nil
		for _, field := range *in.Fields {
			input.Fields = append(input.Fields, Field(field))
		}
	}
	saved, err := f.service.SaveCategory(ctx, user, ws, in.ID, input)
	if err != nil {
		return nil, mcpCategory{}, mcpError(err)
	}
	return nil, categoryToMCP(saved), nil
}

func (f *Feature) mcpCategoryDelete(ctx context.Context, _ *mcp.CallToolRequest, in mcpReferenceIDInput) (*mcp.CallToolResult, mcpDeletedOutput, error) {
	return f.mcpReferenceDelete(ctx, in, f.service.DeleteCategory)
}

func (f *Feature) mcpManufacturerDelete(ctx context.Context, _ *mcp.CallToolRequest, in mcpReferenceIDInput) (*mcp.CallToolResult, mcpDeletedOutput, error) {
	return f.mcpReferenceDelete(ctx, in, f.service.DeleteManufacturer)
}

func (f *Feature) mcpUnitDelete(ctx context.Context, _ *mcp.CallToolRequest, in mcpReferenceIDInput) (*mcp.CallToolResult, mcpDeletedOutput, error) {
	return f.mcpReferenceDelete(ctx, in, f.service.DeleteUnit)
}

func (f *Feature) mcpReferenceDelete(ctx context.Context, in mcpReferenceIDInput, remove func(context.Context, string, string, string) error) (*mcp.CallToolResult, mcpDeletedOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpDeletedOutput{}, err
	}
	if err := remove(ctx, user, ws, in.ID); err != nil {
		return nil, mcpDeletedOutput{}, mcpError(err)
	}
	return nil, mcpDeletedOutput{ID: in.ID, Deleted: true}, nil
}

func (f *Feature) mcpManufacturerSave(ctx context.Context, _ *mcp.CallToolRequest, in mcpManufacturerSaveInput) (*mcp.CallToolResult, mcpManufacturer, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpManufacturer{}, err
	}
	saved, err := f.service.SaveManufacturer(ctx, user, ws, in.ID, in.Name, in.Website)
	if err != nil {
		return nil, mcpManufacturer{}, mcpError(err)
	}
	return nil, mcpManufacturer{ID: saved.ID, Name: saved.Name, Website: saved.Website}, nil
}

func (f *Feature) mcpUnitSave(ctx context.Context, _ *mcp.CallToolRequest, in mcpUnitSaveInput) (*mcp.CallToolResult, mcpUnit, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpUnit{}, err
	}
	saved, err := f.service.SaveUnit(ctx, user, ws, in.ID, in.Name)
	if err != nil {
		return nil, mcpUnit{}, mcpError(err)
	}
	return nil, mcpUnit(saved), nil
}

func (f *Feature) mcpCatalogsSet(ctx context.Context, _ *mcp.CallToolRequest, in mcpCatalogsSetInput) (*mcp.CallToolResult, mcpCatalogsOutput, error) {
	user, ws, err := f.access(ctx, auth.ScopeCraftWrite, in.Space)
	if err != nil {
		return nil, mcpCatalogsOutput{}, err
	}
	sources, err := f.service.SetCatalogSources(ctx, user, ws, in.Sources)
	if err != nil {
		return nil, mcpCatalogsOutput{}, mcpError(err)
	}
	return nil, mcpCatalogsOutput{Catalogs: catalogsToMCP(sources)}, nil
}
