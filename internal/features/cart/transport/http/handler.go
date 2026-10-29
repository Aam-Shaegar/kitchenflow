package handler

import (
	"net/http"

	core_errors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	core_http_request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	core_http_response "github.com/Aam-Shaegar/kitchenflow/internal/core/http/response"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/service"
)

type AddCartItemRequest struct {
	RestaurantID string `json:"restaurant_id" validate:"required"`
	MenuItemID   string `json:"menu_item_id" validate:"required"`
	Quantity     int    `json:"quantity" validate:"required,min=1"`
}

type UpdateCartItemRequest struct {
	Quantity int `json:"quantity" validate:"required,min=1"`
}

type CartHandler struct {
	svc *service.CartService
}

func NewCartHandler(svc *service.CartService) *CartHandler {
	return &CartHandler{svc: svc}
}

func (h *CartHandler) GetCart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	cart, err := h.svc.GetCart(ctx, userID)
	if err != nil {
		handler.ErrorResponse(err, "failed to get cart")
		return
	}

	handler.JSONResponse(cart, http.StatusOK)
}

func (h *CartHandler) AddItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	var req AddCartItemRequest
	if err := core_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}

	restaurantID, err := core_http_request.ParseUUID(req.RestaurantID)
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}

	menuItemID, err := core_http_request.ParseUUID(req.MenuItemID)
	if err != nil {
		handler.ErrorResponse(err, "invalid menu item ID")
		return
	}

	cart, err := h.svc.AddItem(ctx, userID, restaurantID, menuItemID, req.Quantity)
	if err != nil {
		handler.ErrorResponse(err, "failed to add item")
		return
	}

	handler.JSONResponse(cart, http.StatusCreated)
}

func (h *CartHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	cartItemID, err := core_http_request.ParseUUID(r.PathValue("cart_item_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid cart item ID")
		return
	}

	var req UpdateCartItemRequest
	if err := core_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}

	cart, err := h.svc.UpdateItemQuantity(ctx, userID, cartItemID, req.Quantity)
	if err != nil {
		handler.ErrorResponse(err, "failed to update item")
		return
	}

	handler.JSONResponse(cart, http.StatusOK)
}

func (h *CartHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	cartItemID, err := core_http_request.ParseUUID(r.PathValue("cart_item_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid cart item ID")
		return
	}

	if _, err := h.svc.RemoveItem(ctx, userID, cartItemID); err != nil {
		handler.ErrorResponse(err, "failed to remove item")
		return
	}

	handler.NoContentResponse()
}

func (h *CartHandler) ClearCart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}
	if err := h.svc.ClearCart(ctx, userID); err != nil {
		handler.ErrorResponse(err, "failed to clear cart")
		return
	}
	handler.NoContentResponse()
}
