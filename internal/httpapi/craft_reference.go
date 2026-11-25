package httpapi

import (
	"context"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// Handlers of the configuration of a warehouse: categories, manufacturers,
// units and official catalogues.

// GetCraftReference implements GET /api/v1/workspaces/{id}/reference.
func (h *handlers) GetCraftReference(ctx context.Context, request gen.GetCraftReferenceRequestObject) (gen.GetCraftReferenceResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.GetCraftReference401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	reference := gen.GetCraftReference200JSONResponse{
		Categories: []gen.CraftCategory{}, Manufacturers: []gen.CraftManufacturer{},
		Units: []gen.CraftUnit{}, Catalogs: []gen.CraftCatalogSource{},
	}
	categories, err := h.deps.Craft.Categories(ctx, principal, request.Id)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.GetCraftReference400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.GetCraftReference404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	for _, category := range categories {
		reference.Categories = append(reference.Categories, categoryToAPI(category))
	}
	manufacturers, err := h.deps.Craft.Manufacturers(ctx, principal, request.Id)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.GetCraftReference400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.GetCraftReference404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	for _, manufacturer := range manufacturers {
		reference.Manufacturers = append(reference.Manufacturers, manufacturerToAPI(manufacturer))
	}
	units, err := h.deps.Craft.ListUnits(ctx, principal, request.Id)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.GetCraftReference400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.GetCraftReference404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	for _, unit := range units {
		reference.Units = append(reference.Units, gen.CraftUnit{Id: unit.ID, Name: unit.Name})
	}
	sources, err := h.deps.Craft.CatalogSources(ctx, principal, request.Id)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.GetCraftReference400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.GetCraftReference404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	reference.Catalogs = sourcesToAPI(sources)
	return reference, nil
}

// SetCraftCatalogSources implements PUT /api/v1/workspaces/{id}/catalog-sources.
func (h *handlers) SetCraftCatalogSources(ctx context.Context, request gen.SetCraftCatalogSourcesRequestObject) (gen.SetCraftCatalogSourcesResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.SetCraftCatalogSources401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.SetCraftCatalogSources400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	sources, err := h.deps.Craft.SetCatalogSources(ctx, principal, request.Id, request.Body.Sources)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.SetCraftCatalogSources400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.SetCraftCatalogSources404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.SetCraftCatalogSources200JSONResponse(sourcesToAPI(sources)), nil
}

// CreateCraftCategory implements POST /api/v1/workspaces/{id}/categories.
func (h *handlers) CreateCraftCategory(ctx context.Context, request gen.CreateCraftCategoryRequestObject) (gen.CreateCraftCategoryResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateCraftCategory401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.CreateCraftCategory400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	saved, err := h.deps.Craft.SaveCategory(ctx, principal, request.Id, "", categoryInput(*request.Body))
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.CreateCraftCategory400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.CreateCraftCategory404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.CreateCraftCategory201JSONResponse(categoryToAPI(saved)), nil
}

// UpdateCraftCategory implements PUT /api/v1/workspaces/{id}/categories/{ref_id}.
func (h *handlers) UpdateCraftCategory(ctx context.Context, request gen.UpdateCraftCategoryRequestObject) (gen.UpdateCraftCategoryResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateCraftCategory401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.UpdateCraftCategory400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	saved, err := h.deps.Craft.SaveCategory(ctx, principal, request.Id, request.RefId, categoryInput(*request.Body))
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UpdateCraftCategory400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.UpdateCraftCategory404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.UpdateCraftCategory200JSONResponse(categoryToAPI(saved)), nil
}

// DeleteCraftCategory implements DELETE /api/v1/workspaces/{id}/categories/{ref_id}.
func (h *handlers) DeleteCraftCategory(ctx context.Context, request gen.DeleteCraftCategoryRequestObject) (gen.DeleteCraftCategoryResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteCraftCategory401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	err := h.deps.Craft.DeleteCategory(ctx, principal, request.Id, request.RefId)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.DeleteCraftCategory400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.DeleteCraftCategory404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.DeleteCraftCategory204Response{}, nil
}

// CreateCraftManufacturer implements POST /api/v1/workspaces/{id}/manufacturers.
func (h *handlers) CreateCraftManufacturer(ctx context.Context, request gen.CreateCraftManufacturerRequestObject) (gen.CreateCraftManufacturerResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateCraftManufacturer401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.CreateCraftManufacturer400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	saved, err := h.deps.Craft.SaveManufacturer(ctx, principal, request.Id, "", request.Body.Name, request.Body.Website)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.CreateCraftManufacturer400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.CreateCraftManufacturer404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.CreateCraftManufacturer201JSONResponse(manufacturerToAPI(saved)), nil
}

// UpdateCraftManufacturer implements PUT /api/v1/workspaces/{id}/manufacturers/{ref_id}.
func (h *handlers) UpdateCraftManufacturer(ctx context.Context, request gen.UpdateCraftManufacturerRequestObject) (gen.UpdateCraftManufacturerResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateCraftManufacturer401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.UpdateCraftManufacturer400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	saved, err := h.deps.Craft.SaveManufacturer(ctx, principal, request.Id, request.RefId, request.Body.Name, request.Body.Website)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UpdateCraftManufacturer400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.UpdateCraftManufacturer404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.UpdateCraftManufacturer200JSONResponse(manufacturerToAPI(saved)), nil
}

// DeleteCraftManufacturer implements DELETE /api/v1/workspaces/{id}/manufacturers/{ref_id}.
func (h *handlers) DeleteCraftManufacturer(ctx context.Context, request gen.DeleteCraftManufacturerRequestObject) (gen.DeleteCraftManufacturerResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteCraftManufacturer401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	err := h.deps.Craft.DeleteManufacturer(ctx, principal, request.Id, request.RefId)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.DeleteCraftManufacturer400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.DeleteCraftManufacturer404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.DeleteCraftManufacturer204Response{}, nil
}

// CreateCraftUnit implements POST /api/v1/workspaces/{id}/units.
func (h *handlers) CreateCraftUnit(ctx context.Context, request gen.CreateCraftUnitRequestObject) (gen.CreateCraftUnitResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateCraftUnit401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.CreateCraftUnit400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	saved, err := h.deps.Craft.SaveUnit(ctx, principal, request.Id, "", request.Body.Name)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.CreateCraftUnit400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.CreateCraftUnit404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.CreateCraftUnit201JSONResponse(gen.CraftUnit{Id: saved.ID, Name: saved.Name}), nil
}

// UpdateCraftUnit implements PUT /api/v1/workspaces/{id}/units/{ref_id}.
func (h *handlers) UpdateCraftUnit(ctx context.Context, request gen.UpdateCraftUnitRequestObject) (gen.UpdateCraftUnitResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateCraftUnit401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	if request.Body == nil {
		return gen.UpdateCraftUnit400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	saved, err := h.deps.Craft.SaveUnit(ctx, principal, request.Id, request.RefId, request.Body.Name)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UpdateCraftUnit400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.UpdateCraftUnit404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.UpdateCraftUnit200JSONResponse(gen.CraftUnit{Id: saved.ID, Name: saved.Name}), nil
}

// DeleteCraftUnit implements DELETE /api/v1/workspaces/{id}/units/{ref_id}.
func (h *handlers) DeleteCraftUnit(ctx context.Context, request gen.DeleteCraftUnitRequestObject) (gen.DeleteCraftUnitResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteCraftUnit401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired))}, nil
	}
	err := h.deps.Craft.DeleteUnit(ctx, principal, request.Id, request.RefId)
	if err != nil {
		status, message := craftFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.DeleteCraftUnit400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.DeleteCraftUnit404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message))}, nil
		}
		return nil, err
	}
	return gen.DeleteCraftUnit204Response{}, nil
}

func categoryInput(body gen.CraftCategoryInput) craft.CategoryInput {
	input := craft.CategoryInput{Name: body.Name, DefaultUnit: body.DefaultUnit}
	for _, field := range body.Fields {
		converted := craft.Field{Key: stringParam(field.Key), Label: field.Label, Type: string(field.Type)}
		if field.Options != nil {
			converted.Options = *field.Options
		}
		input.Fields = append(input.Fields, converted)
	}
	return input
}

func categoryToAPI(category craft.Category) gen.CraftCategory {
	fields := make([]gen.CraftField, 0, len(category.Fields))
	for _, field := range category.Fields {
		key := field.Key
		converted := gen.CraftField{Key: &key, Label: field.Label, Type: gen.CraftFieldType(field.Type)}
		if len(field.Options) > 0 {
			options := field.Options
			converted.Options = &options
		}
		fields = append(fields, converted)
	}
	return gen.CraftCategory{
		Id: category.ID, Slug: category.Slug, Name: category.Name, DefaultUnit: category.DefaultUnit,
		Fields: fields, CatalogSlug: category.CatalogSlug, OwnItems: category.Items,
	}
}

func manufacturerToAPI(m craft.Manufacturer) gen.CraftManufacturer {
	return gen.CraftManufacturer{Id: m.ID, Name: m.Name, Website: m.Website, Official: m.Official, OwnItems: m.Items}
}

func sourcesToAPI(sources []craft.CatalogSource) []gen.CraftCatalogSource {
	out := make([]gen.CraftCatalogSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, gen.CraftCatalogSource{Source: source.Source, Name: source.Name, Enabled: source.Enabled, Items: source.Items})
	}
	return out
}
