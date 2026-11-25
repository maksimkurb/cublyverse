package httpapi

import (
	"context"
	"errors"

	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// User-facing messages of the workspace endpoints.
const (
	msgBadRequest          = "Некорректный запрос"
	msgWorkspaceNotFound   = "Пространство не найдено"
	msgWorkspaceUserAbsent = "Пользователь не найден"
	msgWorkspaceLastMember = "Нельзя удалить последнего участника: удалите пространство"
)

// ListWorkspaces implements GET /api/v1/workspaces.
func (h *handlers) ListWorkspaces(ctx context.Context, _ gen.ListWorkspacesRequestObject) (gen.ListWorkspacesResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListWorkspaces401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	items, err := h.deps.Workspaces.List(ctx, principal)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListWorkspaces200JSONResponse, 0, len(items))
	for _, item := range items {
		out = append(out, workspaceToAPI(item))
	}
	return out, nil
}

// CreateWorkspace implements POST /api/v1/workspaces.
func (h *handlers) CreateWorkspace(ctx context.Context, request gen.CreateWorkspaceRequestObject) (gen.CreateWorkspaceResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateWorkspace401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.CreateWorkspace400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	created, err := h.deps.Workspaces.Create(ctx, principal, request.Body.Name)
	if err != nil {
		var invalid *workspace.ValidationError
		if errors.As(err, &invalid) {
			return gen.CreateWorkspace400JSONResponse(errorPayload(codeBadRequest, invalid.Message)), nil
		}
		return nil, err
	}
	return gen.CreateWorkspace201JSONResponse(workspaceToAPI(created)), nil
}

// RenameWorkspace implements PATCH /api/v1/workspaces/{id}.
func (h *handlers) RenameWorkspace(ctx context.Context, request gen.RenameWorkspaceRequestObject) (gen.RenameWorkspaceResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.RenameWorkspace401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.RenameWorkspace400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	renamed, err := h.deps.Workspaces.Rename(ctx, principal, request.Id, request.Body.Name)
	if err != nil {
		var invalid *workspace.ValidationError
		switch {
		case errors.As(err, &invalid):
			return gen.RenameWorkspace400JSONResponse(errorPayload(codeBadRequest, invalid.Message)), nil
		case errors.Is(err, workspace.ErrNotFound):
			return gen.RenameWorkspace404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgWorkspaceNotFound)),
			}, nil
		}
		return nil, err
	}
	return gen.RenameWorkspace200JSONResponse(workspaceToAPI(renamed)), nil
}

// DeleteWorkspace implements DELETE /api/v1/workspaces/{id}.
func (h *handlers) DeleteWorkspace(ctx context.Context, request gen.DeleteWorkspaceRequestObject) (gen.DeleteWorkspaceResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteWorkspace401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Workspaces.Delete(ctx, principal, request.Id); err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			return gen.DeleteWorkspace404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgWorkspaceNotFound)),
			}, nil
		}
		return nil, err
	}
	return gen.DeleteWorkspace204Response{}, nil
}

// ListWorkspaceMembers implements GET /api/v1/workspaces/{id}/members.
func (h *handlers) ListWorkspaceMembers(ctx context.Context, request gen.ListWorkspaceMembersRequestObject) (gen.ListWorkspaceMembersResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListWorkspaceMembers401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	members, err := h.deps.Workspaces.Members(ctx, principal, request.Id)
	if err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			return gen.ListWorkspaceMembers404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgWorkspaceNotFound)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.ListWorkspaceMembers200JSONResponse, 0, len(members))
	for _, member := range members {
		out = append(out, memberToAPI(member))
	}
	return out, nil
}

// AddWorkspaceMember implements POST /api/v1/workspaces/{id}/members.
func (h *handlers) AddWorkspaceMember(ctx context.Context, request gen.AddWorkspaceMemberRequestObject) (gen.AddWorkspaceMemberResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.AddWorkspaceMember401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.AddWorkspaceMember400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	err := h.deps.Workspaces.AddMember(ctx, principal, request.Id, request.Body.UserId)
	switch {
	case err == nil:
		return gen.AddWorkspaceMember204Response{}, nil
	case errors.Is(err, workspace.ErrUserNotFound):
		return gen.AddWorkspaceMember404JSONResponse(errorPayload(codeNotFound, msgWorkspaceUserAbsent)), nil
	case errors.Is(err, workspace.ErrNotFound):
		return gen.AddWorkspaceMember404JSONResponse(errorPayload(codeNotFound, msgWorkspaceNotFound)), nil
	}
	return nil, err
}

// RemoveWorkspaceMember implements DELETE /api/v1/workspaces/{id}/members/{user_id}.
func (h *handlers) RemoveWorkspaceMember(ctx context.Context, request gen.RemoveWorkspaceMemberRequestObject) (gen.RemoveWorkspaceMemberResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.RemoveWorkspaceMember401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	err := h.deps.Workspaces.RemoveMember(ctx, principal, request.Id, request.UserId)
	switch {
	case err == nil:
		return gen.RemoveWorkspaceMember204Response{}, nil
	case errors.Is(err, workspace.ErrLastMember):
		return gen.RemoveWorkspaceMember409JSONResponse(errorPayload(codeConflict, msgWorkspaceLastMember)), nil
	case errors.Is(err, workspace.ErrUserNotFound):
		return gen.RemoveWorkspaceMember404JSONResponse(errorPayload(codeNotFound, msgWorkspaceUserAbsent)), nil
	case errors.Is(err, workspace.ErrNotFound):
		return gen.RemoveWorkspaceMember404JSONResponse(errorPayload(codeNotFound, msgWorkspaceNotFound)), nil
	}
	return nil, err
}

// ListWorkspaceMemberCandidates implements GET /api/v1/workspaces/{id}/member-candidates.
func (h *handlers) ListWorkspaceMemberCandidates(ctx context.Context, request gen.ListWorkspaceMemberCandidatesRequestObject) (gen.ListWorkspaceMemberCandidatesResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListWorkspaceMemberCandidates401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	query := ""
	if request.Params.Query != nil {
		query = *request.Params.Query
	}
	candidates, err := h.deps.Workspaces.Candidates(ctx, principal, request.Id, query)
	if err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			return gen.ListWorkspaceMemberCandidates404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgWorkspaceNotFound)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.ListWorkspaceMemberCandidates200JSONResponse, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidateToAPI(candidate))
	}
	return out, nil
}

func workspaceToAPI(item workspace.Workspace) gen.Workspace {
	return gen.Workspace{Id: item.ID, Name: item.Name, CreatedAt: item.CreatedAt}
}

func memberToAPI(member workspace.Member) gen.WorkspaceMember {
	return gen.WorkspaceMember{
		UserId:      member.UserID,
		DisplayName: member.DisplayName,
		Email:       member.Email,
		AvatarUrl:   member.AvatarURL,
		JoinedAt:    member.JoinedAt,
	}
}

func candidateToAPI(candidate workspace.Candidate) gen.WorkspaceCandidate {
	return gen.WorkspaceCandidate{
		UserId:      candidate.UserID,
		DisplayName: candidate.DisplayName,
		Email:       candidate.Email,
		AvatarUrl:   candidate.AvatarURL,
	}
}
