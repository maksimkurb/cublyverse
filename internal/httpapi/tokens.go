package httpapi

import (
	"context"
	"errors"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// User-facing messages of the token endpoints.
const (
	msgTokenNotFound     = "Токен не найден"
	msgTokenSessionOnly  = "Это действие доступно только входу в приложении или браузере"
	msgTokenInsufficient = "У токена недостаточно прав для этого действия"
)

// ListTokens implements GET /api/v1/tokens. A personal access token is rejected
// by the request middleware before it reaches this handler.
func (h *handlers) ListTokens(ctx context.Context, _ gen.ListTokensRequestObject) (gen.ListTokensResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok || h.deps.Auth == nil {
		return gen.ListTokens401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	tokens, err := h.deps.Auth.ListAPITokens(ctx, principal)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListTokens200JSONResponse, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, apiTokenToAPI(token))
	}
	return out, nil
}

// CreateToken implements POST /api/v1/tokens. The plaintext is returned once;
// the database keeps only its sha256.
func (h *handlers) CreateToken(ctx context.Context, request gen.CreateTokenRequestObject) (gen.CreateTokenResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok || h.deps.Auth == nil {
		return gen.CreateToken401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.CreateToken400JSONResponse(errorPayload(codeBadRequest, "Некорректный запрос")), nil
	}
	scopes := make([]string, 0, len(request.Body.Scopes))
	for _, scope := range request.Body.Scopes {
		scopes = append(scopes, string(scope))
	}
	token, plaintext, err := h.deps.Auth.CreateAPIToken(
		ctx, principal, request.Body.Name, scopes, request.Body.ExpiresInDays,
	)
	if err != nil {
		var invalid *auth.APITokenValidationError
		if errors.As(err, &invalid) {
			return gen.CreateToken400JSONResponse(errorPayload(codeBadRequest, invalid.Message)), nil
		}
		return nil, err
	}
	return gen.CreateToken201JSONResponse(gen.CreatedToken{
		Token:    plaintext,
		ApiToken: apiTokenToAPI(token),
	}), nil
}

// RevokeToken implements DELETE /api/v1/tokens/{id}.
func (h *handlers) RevokeToken(ctx context.Context, request gen.RevokeTokenRequestObject) (gen.RevokeTokenResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok || h.deps.Auth == nil {
		return gen.RevokeToken401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Auth.RevokeAPIToken(ctx, principal, request.Id); err != nil {
		if errors.Is(err, auth.ErrAPITokenNotFound) {
			return gen.RevokeToken404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, msgTokenNotFound)),
			}, nil
		}
		return nil, err
	}
	return gen.RevokeToken204Response{}, nil
}

// apiTokenToAPI maps the service model to the API schema. The plaintext is
// never part of the token model, so it cannot leak through a list response.
func apiTokenToAPI(token auth.APIToken) gen.APIToken {
	scopes := make([]gen.APITokenScope, 0, len(token.Scopes))
	for _, scope := range token.Scopes {
		scopes = append(scopes, gen.APITokenScope(scope))
	}
	return gen.APIToken{
		Id:         token.ID,
		Name:       token.Name,
		Prefix:     token.Prefix,
		Scopes:     scopes,
		ExpiresAt:  token.ExpiresAt,
		LastUsedAt: token.LastUsedAt,
		RevokedAt:  token.RevokedAt,
		CreatedAt:  token.CreatedAt,
	}
}
