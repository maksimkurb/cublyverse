package httpapi

import (
	"context"

	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
	"github.com/maksimkurb/cublyverse/internal/oauthserver"
)

const msgConnectionNotFound = "Подключение не найдено"

// ListConnections implements GET /api/v1/connections.
func (h *handlers) ListConnections(ctx context.Context, _ gen.ListConnectionsRequestObject) (gen.ListConnectionsResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListConnections401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	out := gen.ListConnections200JSONResponse{}
	if h.deps.Connections == nil {
		return out, nil
	}
	grants, err := h.deps.Connections.Grants(ctx, principal)
	if err != nil {
		return nil, err
	}
	for _, grant := range grants {
		out = append(out, connectionToAPI(grant))
	}
	return out, nil
}

// RevokeConnection implements DELETE /api/v1/connections/{id}.
func (h *handlers) RevokeConnection(ctx context.Context, request gen.RevokeConnectionRequestObject) (gen.RevokeConnectionResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.RevokeConnection401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	revoked := false
	if h.deps.Connections != nil {
		var err error
		if revoked, err = h.deps.Connections.RevokeGrant(ctx, principal, request.Id); err != nil {
			return nil, err
		}
	}
	if !revoked {
		return gen.RevokeConnection404JSONResponse{
			NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgConnectionNotFound)),
		}, nil
	}
	return gen.RevokeConnection204Response{}, nil
}

func connectionToAPI(grant oauthserver.Grant) gen.Connection {
	return gen.Connection{
		Id:         grant.ID,
		ClientName: grant.ClientName,
		Scopes:     grant.Scopes,
		CreatedAt:  grant.CreatedAt,
		LastUsedAt: grant.LastUsedAt,
	}
}
