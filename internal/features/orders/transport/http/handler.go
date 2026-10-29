package handler

import (
	"net/http"
	"strconv"

	core_errors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	core_http_request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	core_http_response "github.com/Aam-Shaegar/kitchenflow/internal/core/http/response"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/service"
)

type CreateOrderRequest struct {
	DeliveryAddress string `json:"delivery_address" validate:"required,min=5,max=500"`
}

type OrderHandler struct {
	svc *service.OrderService
}

func NewOrderHandler(svc *service.OrderService) *OrderHandler {
	return &OrderHandler{svc: svc}
}

func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing Idempotency-Key header")
		return
	}

	var req CreateOrderRequest
	if err := core_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}

	order, err := h.svc.CreateOrder(ctx, userID, req.DeliveryAddress, idempotencyKey)
	if err != nil {
		handler.ErrorResponse(err, "failed to create order")
		return
	}

	handler.JSONResponse(order, http.StatusCreated)
}

func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	id, err := core_http_request.ParseUUID(r.PathValue("order_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid order ID")
		return
	}

	order, err := h.svc.GetOrder(ctx, id, userID)
	if err != nil {
		handler.ErrorResponse(err, "failed to get order")
		return
	}

	handler.JSONResponse(order, http.StatusOK)
}

func (h *OrderHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	limit, offset := parsePagination(r)
	orders, err := h.svc.GetOrdersByUserIDPaginated(ctx, userID, limit, offset)
	if err != nil {
		handler.ErrorResponse(err, "failed to get orders")
		return
	}

	handler.JSONResponse(orders, http.StatusOK)
}

func (h *OrderHandler) GetOrderStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}

	id, err := core_http_request.ParseUUID(r.PathValue("order_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid order ID")
		return
	}

	status, err := h.svc.GetOrderStatus(ctx, id, userID)
	if err != nil {
		handler.ErrorResponse(err, "failed to get order status")
		return
	}

	handler.JSONResponse(map[string]string{"status": string(status)}, http.StatusOK)
}

func (h *OrderHandler) GetOrderHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}
	id, err := core_http_request.ParseUUID(r.PathValue("order_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid order ID")
		return
	}
	history, err := h.svc.GetOrderHistory(ctx, id, userID)
	if err != nil {
		handler.ErrorResponse(err, "failed to get order history")
		return
	}
	handler.JSONResponse(history, http.StatusOK)
}

func (h *OrderHandler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}
	id, err := core_http_request.ParseUUID(r.PathValue("order_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid order ID")
		return
	}
	order, err := h.svc.CancelOrder(ctx, id, userID)
	if err != nil {
		handler.ErrorResponse(err, "failed to cancel order")
		return
	}
	handler.JSONResponse(order, http.StatusOK)
}

func (h *OrderHandler) RetryOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		handler.ErrorResponse(core_errors.ErrInvalidArgument, "missing X-User-ID header")
		return
	}
	id, err := core_http_request.ParseUUID(r.PathValue("order_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid order ID")
		return
	}
	order, err := h.svc.RetryIntegration(ctx, id, userID)
	if err != nil {
		handler.ErrorResponse(err, "failed to retry order")
		return
	}
	handler.JSONResponse(order, http.StatusOK)
}

func (h *OrderHandler) ListRestaurantOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	restaurantID, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}
	limit, offset := parsePagination(r)
	orders, err := h.svc.GetOrdersByRestaurantID(ctx, restaurantID, limit, offset)
	if err != nil {
		handler.ErrorResponse(err, "failed to list restaurant orders")
		return
	}
	handler.JSONResponse(orders, http.StatusOK)
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 20
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}
