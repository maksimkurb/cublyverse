package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// User-facing messages of the project endpoints.
const (
	msgProjectNotFound = "Проект не найден"
)

// ListProjects implements GET /api/v1/workspaces/{id}/projects.
func (h *handlers) ListProjects(ctx context.Context, request gen.ListProjectsRequestObject) (gen.ListProjectsResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListProjects401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	projects, err := h.deps.Craft.ListProjects(ctx, principal, request.Id)
	if err != nil {
		status, message := projectFailure(err)
		if status == http.StatusNotFound {
			return gen.ListProjects404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.ListProjects200JSONResponse, 0, len(projects))
	for _, project := range projects {
		out = append(out, projectToAPI(project))
	}
	return out, nil
}

// CreateProject implements POST /api/v1/workspaces/{id}/projects.
func (h *handlers) CreateProject(ctx context.Context, request gen.CreateProjectRequestObject) (gen.CreateProjectResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateProject401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.CreateProject400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	created, err := h.deps.Craft.CreateProject(ctx, principal, request.Id, projectInput(*request.Body))
	if err != nil {
		status, message := projectFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.CreateProject400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.CreateProject404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.CreateProject201JSONResponse(projectToAPI(created)), nil
}

// GetProject implements GET /api/v1/workspaces/{id}/projects/{project_id}.
func (h *handlers) GetProject(ctx context.Context, request gen.GetProjectRequestObject) (gen.GetProjectResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.GetProject401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	project, err := h.deps.Craft.GetProject(ctx, principal, request.Id, request.ProjectId)
	if err != nil {
		status, message := projectFailure(err)
		if status == http.StatusNotFound {
			return gen.GetProject404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.GetProject200JSONResponse(projectToAPI(project)), nil
}

// UpdateProject implements PATCH /api/v1/workspaces/{id}/projects/{project_id}.
func (h *handlers) UpdateProject(ctx context.Context, request gen.UpdateProjectRequestObject) (gen.UpdateProjectResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateProject401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UpdateProject400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	updated, err := h.deps.Craft.UpdateProject(ctx, principal, request.Id, request.ProjectId, projectInput(*request.Body))
	if err != nil {
		status, message := projectFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UpdateProject400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.UpdateProject404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.UpdateProject200JSONResponse(projectToAPI(updated)), nil
}

// DeleteProject implements DELETE /api/v1/workspaces/{id}/projects/{project_id}.
func (h *handlers) DeleteProject(ctx context.Context, request gen.DeleteProjectRequestObject) (gen.DeleteProjectResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteProject401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Craft.DeleteProject(ctx, principal, request.Id, request.ProjectId); err != nil {
		status, message := projectFailure(err)
		if status == http.StatusNotFound {
			return gen.DeleteProject404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.DeleteProject204Response{}, nil
}

// SetProjectMaterial implements PUT /api/v1/workspaces/{id}/projects/{project_id}/materials/{item_id}.
func (h *handlers) SetProjectMaterial(ctx context.Context, request gen.SetProjectMaterialRequestObject) (gen.SetProjectMaterialResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.SetProjectMaterial401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.SetProjectMaterial400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	project, err := h.deps.Craft.SetProjectMaterial(ctx, principal, request.Id, request.ProjectId, request.ItemId, request.Body.QtyPlanned)
	if err != nil {
		status, message := projectFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.SetProjectMaterial400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.SetProjectMaterial404JSONResponse(errorPayload(codeNotFound, message)), nil
		}
		return nil, err
	}
	return gen.SetProjectMaterial200JSONResponse(projectToAPI(project)), nil
}

// RemoveProjectMaterial implements DELETE /api/v1/workspaces/{id}/projects/{project_id}/materials/{item_id}.
func (h *handlers) RemoveProjectMaterial(ctx context.Context, request gen.RemoveProjectMaterialRequestObject) (gen.RemoveProjectMaterialResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.RemoveProjectMaterial401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Craft.RemoveProjectMaterial(ctx, principal, request.Id, request.ProjectId, request.ItemId); err != nil {
		status, message := projectFailure(err)
		if status == http.StatusNotFound {
			return gen.RemoveProjectMaterial404JSONResponse(errorPayload(codeNotFound, message)), nil
		}
		return nil, err
	}
	return gen.RemoveProjectMaterial204Response{}, nil
}

// UseProjectMaterial implements POST /api/v1/workspaces/{id}/projects/{project_id}/materials/{item_id}/use.
func (h *handlers) UseProjectMaterial(ctx context.Context, request gen.UseProjectMaterialRequestObject) (gen.UseProjectMaterialResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UseProjectMaterial401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UseProjectMaterial400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	project, err := h.deps.Craft.UseMaterial(ctx, principal, request.Id, request.ProjectId, request.ItemId, request.Body.Qty)
	if err != nil {
		status, message := projectFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UseProjectMaterial400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.UseProjectMaterial404JSONResponse(errorPayload(codeNotFound, message)), nil
		case http.StatusConflict:
			return gen.UseProjectMaterial409JSONResponse(errorPayload(codeConflict, message)), nil
		}
		return nil, err
	}
	return gen.UseProjectMaterial200JSONResponse(projectToAPI(project)), nil
}

// ShortageToWishlist implements POST /api/v1/workspaces/{id}/projects/{project_id}/shortage-to-wishlist.
func (h *handlers) ShortageToWishlist(ctx context.Context, request gen.ShortageToWishlistRequestObject) (gen.ShortageToWishlistResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ShortageToWishlist401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	added, err := h.deps.Craft.ShortageToWishlist(ctx, principal, request.Id, request.ProjectId)
	if err != nil {
		status, message := projectFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.ShortageToWishlist400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.ShortageToWishlist404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.ShortageToWishlist200JSONResponse, 0, len(added))
	for _, wish := range added {
		out = append(out, wishToAPI(wish))
	}
	return out, nil
}

// projectInput maps the request body to the service input.
func projectInput(body gen.ProjectInput) craft.ProjectInput {
	return craft.ProjectInput{Name: body.Name, Notes: stringParam(body.Notes)}
}

// projectToAPI maps a project of the service to the API schema.
func projectToAPI(project craft.Project) gen.Project {
	materials := make([]gen.ProjectMaterial, 0, len(project.Materials))
	for _, material := range project.Materials {
		materials = append(materials, gen.ProjectMaterial{
			Item:       craftItemToAPI(material.Item),
			QtyPlanned: material.QtyPlanned,
			QtyUsed:    material.QtyUsed,
			Shortage:   material.Shortage,
		})
	}
	return gen.Project{
		Id:            project.ID,
		Name:          project.Name,
		Notes:         project.Notes,
		CreatedAt:     project.CreatedAt,
		MaterialCount: project.MaterialCount,
		Materials:     materials,
	}
}

// projectFailure maps a service error of the projects to a status and a
// user-facing message. Zero means the error is unexpected and becomes a 500.
func projectFailure(err error) (int, string) {
	var invalid *craft.ValidationError
	switch {
	case errors.As(err, &invalid):
		return http.StatusBadRequest, invalid.Message
	case errors.Is(err, workspace.ErrNotFound):
		return http.StatusNotFound, msgWorkspaceNotFound
	case errors.Is(err, craft.ErrNotFound):
		return http.StatusNotFound, msgProjectNotFound
	case errors.Is(err, craft.ErrInsufficientStock):
		return http.StatusConflict, msgCraftNotEnoughStock
	default:
		return 0, ""
	}
}
