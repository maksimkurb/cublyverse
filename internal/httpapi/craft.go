package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// User-facing messages of the craft endpoints.
const (
	msgCraftItemNotFound   = "Позиция не найдена"
	msgCraftReadOnly       = "Позиция общего каталога доступна только для чтения"
	msgCraftStockNotFound  = "Позиция не найдена на складе"
	msgCraftNotEnoughStock = "Недостаточно запаса для списания"
)

// ListCraftCategories implements GET /api/v1/craft/categories.
func (h *handlers) ListCraftCategories(ctx context.Context, _ gen.ListCraftCategoriesRequestObject) (gen.ListCraftCategoriesResponseObject, error) {
	if _, ok := h.principalID(ctx); !ok {
		return gen.ListCraftCategories401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	categories, err := h.deps.Craft.Categories(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListCraftCategories200JSONResponse, 0, len(categories))
	for _, category := range categories {
		out = append(out, gen.CraftCategory{
			Id:          category.ID,
			Slug:        category.Slug,
			Name:        category.Name,
			DefaultUnit: category.DefaultUnit,
		})
	}
	return out, nil
}

// ListCraftManufacturers implements GET /api/v1/craft/manufacturers.
func (h *handlers) ListCraftManufacturers(ctx context.Context, _ gen.ListCraftManufacturersRequestObject) (gen.ListCraftManufacturersResponseObject, error) {
	if _, ok := h.principalID(ctx); !ok {
		return gen.ListCraftManufacturers401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	manufacturers, err := h.deps.Craft.Manufacturers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListCraftManufacturers200JSONResponse, 0, len(manufacturers))
	for _, manufacturer := range manufacturers {
		out = append(out, gen.CraftManufacturer{
			Id:      manufacturer.ID,
			Name:    manufacturer.Name,
			Website: manufacturer.Website,
		})
	}
	return out, nil
}

// SearchCraftCatalog implements GET /api/v1/workspaces/{id}/catalog.
func (h *handlers) SearchCraftCatalog(ctx context.Context, request gen.SearchCraftCatalogRequestObject) (gen.SearchCraftCatalogResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.SearchCraftCatalog401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	filter := craft.CatalogFilter{
		Category:     stringParam(request.Params.Category),
		Manufacturer: stringParam(request.Params.Manufacturer),
		Query:        stringParam(request.Params.Q),
		OnlyInStock:  request.Params.OnlyInStock != nil && *request.Params.OnlyInStock,
		Limit:        intParam(request.Params.Limit),
		Offset:       intParam(request.Params.Offset),
	}
	items, err := h.deps.Craft.SearchCatalog(ctx, principal, request.Id, filter)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusNotFound:
			return gen.SearchCraftCatalog404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.SearchCraftCatalog200JSONResponse, 0, len(items))
	for _, item := range items {
		out = append(out, craftItemToAPI(item))
	}
	return out, nil
}

// CreateCraftItem implements POST /api/v1/workspaces/{id}/catalog.
func (h *handlers) CreateCraftItem(ctx context.Context, request gen.CreateCraftItemRequestObject) (gen.CreateCraftItemResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateCraftItem401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.CreateCraftItem400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	created, err := h.deps.Craft.CreateItem(ctx, principal, request.Id, craftItemInput(*request.Body))
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.CreateCraftItem400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.CreateCraftItem404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.CreateCraftItem201JSONResponse(craftItemToAPI(created)), nil
}

// GetCraftItem implements GET /api/v1/workspaces/{id}/catalog/{item_id}.
func (h *handlers) GetCraftItem(ctx context.Context, request gen.GetCraftItemRequestObject) (gen.GetCraftItemResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.GetCraftItem401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	item, err := h.deps.Craft.GetItem(ctx, principal, request.Id, request.ItemId)
	if err != nil {
		status, message := craftFailure(err)
		if status == http.StatusNotFound {
			return gen.GetCraftItem404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.GetCraftItem200JSONResponse(craftItemToAPI(item)), nil
}

// UpdateCraftItem implements PUT /api/v1/workspaces/{id}/catalog/{item_id}.
func (h *handlers) UpdateCraftItem(ctx context.Context, request gen.UpdateCraftItemRequestObject) (gen.UpdateCraftItemResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateCraftItem401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UpdateCraftItem400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	updated, err := h.deps.Craft.UpdateItem(ctx, principal, request.Id, request.ItemId, craftItemInput(*request.Body))
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UpdateCraftItem400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusForbidden:
			return gen.UpdateCraftItem403JSONResponse(errorPayload(codeForbidden, message)), nil
		case http.StatusNotFound:
			return gen.UpdateCraftItem404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.UpdateCraftItem200JSONResponse(craftItemToAPI(updated)), nil
}

// DeleteCraftItem implements DELETE /api/v1/workspaces/{id}/catalog/{item_id}.
func (h *handlers) DeleteCraftItem(ctx context.Context, request gen.DeleteCraftItemRequestObject) (gen.DeleteCraftItemResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteCraftItem401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Craft.DeleteItem(ctx, principal, request.Id, request.ItemId); err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusForbidden:
			return gen.DeleteCraftItem403JSONResponse(errorPayload(codeForbidden, message)), nil
		case http.StatusNotFound:
			return gen.DeleteCraftItem404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.DeleteCraftItem204Response{}, nil
}

// LinkCraftItem implements POST /api/v1/workspaces/{id}/catalog/{item_id}/link.
func (h *handlers) LinkCraftItem(ctx context.Context, request gen.LinkCraftItemRequestObject) (gen.LinkCraftItemResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.LinkCraftItem401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.LinkCraftItem400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	entry, err := h.deps.Craft.LinkToCatalog(ctx, principal, request.Id, request.ItemId, request.Body.TargetItemId)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.LinkCraftItem400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusForbidden:
			return gen.LinkCraftItem403JSONResponse(errorPayload(codeForbidden, message)), nil
		case http.StatusNotFound:
			return gen.LinkCraftItem404JSONResponse(errorPayload(codeNotFound, message)), nil
		}
		return nil, err
	}
	return gen.LinkCraftItem200JSONResponse(craftStockToAPI(entry)), nil
}

// ListCraftInventory implements GET /api/v1/workspaces/{id}/inventory.
func (h *handlers) ListCraftInventory(ctx context.Context, request gen.ListCraftInventoryRequestObject) (gen.ListCraftInventoryResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListCraftInventory401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	filter := craft.CatalogFilter{
		Category:     stringParam(request.Params.Category),
		Manufacturer: stringParam(request.Params.Manufacturer),
		Query:        stringParam(request.Params.Q),
		MaxQty:       request.Params.MaxQty,
		Limit:        intParam(request.Params.Limit),
		Offset:       intParam(request.Params.Offset),
	}
	rows, err := h.deps.Craft.ListInventory(ctx, principal, request.Id, filter)
	if err != nil {
		status, message := craftFailure(err)
		if status == http.StatusNotFound {
			return gen.ListCraftInventory404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.ListCraftInventory200JSONResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, craftStockToAPI(row))
	}
	return out, nil
}

// SetCraftInventoryQty implements PUT /api/v1/workspaces/{id}/inventory/{item_id}.
func (h *handlers) SetCraftInventoryQty(ctx context.Context, request gen.SetCraftInventoryQtyRequestObject) (gen.SetCraftInventoryQtyResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.SetCraftInventoryQty401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.SetCraftInventoryQty400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	entry, err := h.deps.Craft.SetQty(ctx, principal, request.Id, request.ItemId, request.Body.Qty, stringParam(request.Body.Reason))
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.SetCraftInventoryQty400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.SetCraftInventoryQty404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.SetCraftInventoryQty200JSONResponse(craftStockToAPI(entry)), nil
}

// AdjustCraftInventoryQty implements POST /api/v1/workspaces/{id}/inventory/{item_id}/adjust.
func (h *handlers) AdjustCraftInventoryQty(ctx context.Context, request gen.AdjustCraftInventoryQtyRequestObject) (gen.AdjustCraftInventoryQtyResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.AdjustCraftInventoryQty401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.AdjustCraftInventoryQty400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	entry, err := h.deps.Craft.Adjust(ctx, principal, request.Id, request.ItemId, request.Body.Delta, stringParam(request.Body.Reason))
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.AdjustCraftInventoryQty400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.AdjustCraftInventoryQty404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		case http.StatusConflict:
			return gen.AdjustCraftInventoryQty409JSONResponse(errorPayload(codeConflict, message)), nil
		}
		return nil, err
	}
	return gen.AdjustCraftInventoryQty200JSONResponse(craftStockToAPI(entry)), nil
}

// UpdateCraftInventoryMeta implements PATCH /api/v1/workspaces/{id}/inventory/{item_id}.
func (h *handlers) UpdateCraftInventoryMeta(ctx context.Context, request gen.UpdateCraftInventoryMetaRequestObject) (gen.UpdateCraftInventoryMetaResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateCraftInventoryMeta401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UpdateCraftInventoryMeta400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	err := h.deps.Craft.UpdateStockMeta(ctx, principal, request.Id, request.ItemId, request.Body.Location, request.Body.Notes)
	if err != nil {
		status, message := craftFailure(err)
		if status == http.StatusNotFound {
			return gen.UpdateCraftInventoryMeta404JSONResponse(errorPayload(codeNotFound, message)), nil
		}
		return nil, err
	}
	return gen.UpdateCraftInventoryMeta204Response{}, nil
}

// RemoveCraftInventory implements DELETE /api/v1/workspaces/{id}/inventory/{item_id}.
func (h *handlers) RemoveCraftInventory(ctx context.Context, request gen.RemoveCraftInventoryRequestObject) (gen.RemoveCraftInventoryResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.RemoveCraftInventory401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Craft.RemoveFromInventory(ctx, principal, request.Id, request.ItemId); err != nil {
		status, message := craftFailure(err)
		if status == http.StatusNotFound {
			return gen.RemoveCraftInventory404JSONResponse(errorPayload(codeNotFound, message)), nil
		}
		return nil, err
	}
	return gen.RemoveCraftInventory204Response{}, nil
}

// craftItemToAPI maps a catalogue item of the service to the API schema.
func craftItemToAPI(item craft.Item) gen.CraftItem {
	var workspaceID *string
	if item.WorkspaceID != "" {
		id := item.WorkspaceID
		workspaceID = &id
	}
	attrs := item.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	return gen.CraftItem{
		Id:           item.ID,
		WorkspaceId:  workspaceID,
		Private:      item.Private(),
		Category:     item.Category,
		CategoryName: item.CategoryName,
		Manufacturer: item.Manufacturer,
		Article:      item.Article,
		Series:       item.Series,
		Name:         item.Name,
		ColorName:    item.ColorName,
		ColorHex:     item.ColorHex,
		ColorSource:  item.ColorSource,
		Size:         item.Size,
		Finish:       item.Finish,
		Unit:         item.Unit,
		PackSize:     item.PackSize,
		Attrs:        attrs,
		Source:       item.Source,
		SourceUrl:    item.SourceURL,
		ImageUrl:     item.ImageURL,
		StockQty:     item.StockQty,
		InInventory:  item.InInventory,
	}
}

func craftStockToAPI(entry craft.StockEntry) gen.CraftStockEntry {
	return gen.CraftStockEntry{
		Item:      craftItemToAPI(entry.Item),
		Qty:       entry.Qty,
		Location:  entry.Location,
		Notes:     entry.Notes,
		UpdatedAt: entry.UpdatedAt,
	}
}

// craftItemInput maps the request body to the service input.
func craftItemInput(body gen.CraftItemInput) craft.ItemInput {
	input := craft.ItemInput{
		Category:     body.Category,
		Manufacturer: stringParam(body.Manufacturer),
		Article:      stringParam(body.Article),
		Series:       stringParam(body.Series),
		Name:         stringParam(body.Name),
		ColorName:    stringParam(body.ColorName),
		ColorHex:     stringParam(body.ColorHex),
		Size:         stringParam(body.Size),
		Finish:       stringParam(body.Finish),
		Unit:         stringParam(body.Unit),
		PackSize:     body.PackSize,
	}
	if body.ColorSource != nil {
		input.ColorSource = string(*body.ColorSource)
	}
	if body.Attrs != nil {
		input.Attrs = *body.Attrs
	}
	return input
}

// craftFailure maps a service error to a status and a user-facing message. Zero
// means the error is unexpected and has to become a 500.
func craftFailure(err error) (int, string) {
	var invalid *craft.ValidationError
	switch {
	case errors.As(err, &invalid):
		return http.StatusBadRequest, invalid.Message
	case errors.Is(err, workspace.ErrNotFound):
		return http.StatusNotFound, msgWorkspaceNotFound
	case errors.Is(err, craft.ErrNotFound):
		return http.StatusNotFound, msgCraftItemNotFound
	case errors.Is(err, craft.ErrReadOnly):
		return http.StatusForbidden, msgCraftReadOnly
	case errors.Is(err, craft.ErrInsufficientStock):
		return http.StatusConflict, msgCraftNotEnoughStock
	default:
		return 0, ""
	}
}

func stringParam(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intParam(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
