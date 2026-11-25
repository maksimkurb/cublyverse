package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// User-facing messages of the wish list endpoints.
const (
	msgWishNotFound = "Запись в закупках не найдена"
)

// ListWishes implements GET /api/v1/workspaces/{id}/wishlist.
func (h *handlers) ListWishes(ctx context.Context, request gen.ListWishesRequestObject) (gen.ListWishesResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.ListWishes401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	status := ""
	if request.Params.Status != nil {
		status = string(*request.Params.Status)
	}
	wishes, err := h.deps.Craft.ListWishes(ctx, principal, request.Id, craft.WishFilter{Status: status})
	if err != nil {
		status, message := wishFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.ListWishes400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.ListWishes404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	out := make(gen.ListWishes200JSONResponse, 0, len(wishes))
	for _, wish := range wishes {
		out = append(out, wishToAPI(wish))
	}
	return out, nil
}

// CreateWish implements POST /api/v1/workspaces/{id}/wishlist.
func (h *handlers) CreateWish(ctx context.Context, request gen.CreateWishRequestObject) (gen.CreateWishResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.CreateWish401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.CreateWish400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	input := craft.WishInput{
		CatalogItemID: stringParam(request.Body.CatalogItemId),
		Title:         stringParam(request.Body.Title),
		QtyWanted:     request.Body.QtyWanted,
		Note:          stringParam(request.Body.Note),
	}
	if request.Body.Priority != nil {
		input.Priority = *request.Body.Priority
	}
	created, err := h.deps.Craft.AddWish(ctx, principal, request.Id, input)
	if err != nil {
		status, message := wishFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.CreateWish400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.CreateWish404JSONResponse(errorPayload(codeNotFound, message)), nil
		}
		return nil, err
	}
	return gen.CreateWish201JSONResponse(wishToAPI(created)), nil
}

// UpdateWish implements PATCH /api/v1/workspaces/{id}/wishlist/{wish_id}.
func (h *handlers) UpdateWish(ctx context.Context, request gen.UpdateWishRequestObject) (gen.UpdateWishResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.UpdateWish401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UpdateWish400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	input := craft.WishUpdate{
		QtyWanted: request.Body.QtyWanted,
		Priority:  request.Body.Priority,
		Note:      request.Body.Note,
	}
	if request.Body.Status != nil {
		input.Status = string(*request.Body.Status)
	}
	updated, err := h.deps.Craft.UpdateWish(ctx, principal, request.Id, request.WishId, input)
	if err != nil {
		status, message := wishFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.UpdateWish400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.UpdateWish404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.UpdateWish200JSONResponse(wishToAPI(updated)), nil
}

// DeleteWish implements DELETE /api/v1/workspaces/{id}/wishlist/{wish_id}.
func (h *handlers) DeleteWish(ctx context.Context, request gen.DeleteWishRequestObject) (gen.DeleteWishResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.DeleteWish401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Craft.DeleteWish(ctx, principal, request.Id, request.WishId); err != nil {
		status, message := wishFailure(err)
		if status == http.StatusNotFound {
			return gen.DeleteWish404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.DeleteWish204Response{}, nil
}

// AddWishLink implements POST /api/v1/workspaces/{id}/wishlist/{wish_id}/links.
func (h *handlers) AddWishLink(ctx context.Context, request gen.AddWishLinkRequestObject) (gen.AddWishLinkResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.AddWishLink401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.AddWishLink400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	input := craft.WishLinkInput{
		URL:      request.Body.Url,
		ShopName: stringParam(request.Body.ShopName),
		Price:    request.Body.Price,
		Currency: stringParam(request.Body.Currency),
		InStock:  request.Body.InStock,
	}
	created, err := h.deps.Craft.AddWishLink(ctx, principal, request.Id, request.WishId, input)
	if err != nil {
		status, message := wishFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.AddWishLink400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.AddWishLink404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.AddWishLink201JSONResponse(wishLinkToAPI(created)), nil
}

// RemoveWishLink implements DELETE /api/v1/workspaces/{id}/wishlist/{wish_id}/links/{link_id}.
func (h *handlers) RemoveWishLink(ctx context.Context, request gen.RemoveWishLinkRequestObject) (gen.RemoveWishLinkResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.RemoveWishLink401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err := h.deps.Craft.RemoveWishLink(ctx, principal, request.Id, request.WishId, request.LinkId); err != nil {
		status, message := wishFailure(err)
		if status == http.StatusNotFound {
			return gen.RemoveWishLink404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.RemoveWishLink204Response{}, nil
}

// MarkWishBought implements POST /api/v1/workspaces/{id}/wishlist/{wish_id}/bought.
func (h *handlers) MarkWishBought(ctx context.Context, request gen.MarkWishBoughtRequestObject) (gen.MarkWishBoughtResponseObject, error) {
	principal, ok := h.principalID(ctx)
	if !ok {
		return gen.MarkWishBought401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.MarkWishBought400JSONResponse(errorPayload(codeBadRequest, msgBadRequest)), nil
	}
	bought, err := h.deps.Craft.MarkBought(ctx, principal, request.Id, request.WishId, request.Body.AddToStock)
	if err != nil {
		status, message := wishFailure(err)
		switch status {
		case http.StatusBadRequest:
			return gen.MarkWishBought400JSONResponse(errorPayload(codeBadRequest, message)), nil
		case http.StatusNotFound:
			return gen.MarkWishBought404JSONResponse{
				NotFoundJSONResponse: gen.NotFoundJSONResponse(errorPayload(codeNotFound, message)),
			}, nil
		}
		return nil, err
	}
	return gen.MarkWishBought200JSONResponse(wishToAPI(bought)), nil
}

// wishToAPI maps a wish list entry of the service to the API schema.
func wishToAPI(wish craft.Wish) gen.Wish {
	var item *gen.CraftItem
	if wish.Item != nil {
		converted := craftItemToAPI(*wish.Item)
		item = &converted
	}
	links := make([]gen.WishLink, 0, len(wish.Links))
	for _, link := range wish.Links {
		links = append(links, wishLinkToAPI(link))
	}
	return gen.Wish{
		Id:            wish.ID,
		CatalogItemId: wish.CatalogItemID,
		Title:         wish.Title,
		QtyWanted:     wish.QtyWanted,
		Priority:      wish.Priority,
		Note:          wish.Note,
		Status:        gen.WishStatus(wish.Status),
		CreatedBy:     wish.CreatedBy,
		CreatedAt:     wish.CreatedAt,
		UpdatedAt:     wish.UpdatedAt,
		Item:          item,
		Links:         links,
	}
}

func wishLinkToAPI(link craft.WishLink) gen.WishLink {
	return gen.WishLink{
		Id:        link.ID,
		Url:       link.URL,
		ShopName:  link.ShopName,
		Price:     link.Price,
		Currency:  link.Currency,
		InStock:   link.InStock,
		CheckedAt: link.CheckedAt,
		CreatedAt: link.CreatedAt,
	}
}

// wishFailure maps a service error of the wish list to a status and a
// user-facing message. Zero means the error is unexpected and becomes a 500.
func wishFailure(err error) (int, string) {
	var invalid *craft.ValidationError
	switch {
	case errors.As(err, &invalid):
		return http.StatusBadRequest, invalid.Message
	case errors.Is(err, workspace.ErrNotFound):
		return http.StatusNotFound, msgWorkspaceNotFound
	case errors.Is(err, craft.ErrNotFound):
		return http.StatusNotFound, msgWishNotFound
	case errors.Is(err, craft.ErrInsufficientStock):
		return http.StatusConflict, msgCraftNotEnoughStock
	default:
		return 0, ""
	}
}
