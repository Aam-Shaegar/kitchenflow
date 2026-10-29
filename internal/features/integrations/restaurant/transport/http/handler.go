package handler

import (
	"net/http"

	core_http_request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	core_http_response "github.com/Aam-Shaegar/kitchenflow/internal/core/http/response"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/service"
)

type RestaurantCallbackRequest struct {
	ExternalOrderID string `json:"external_order_id" validate:"required"`
	Status          string `json:"status" validate:"required"`
	EventID         string `json:"event_id" validate:"required"`
}

type IntegrationHandler struct {
	svc *service.OrderService
}

func NewIntegrationHandler(svc *service.OrderService) *IntegrationHandler {
	return &IntegrationHandler{svc: svc}
}

func (h *IntegrationHandler) RestaurantCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	restaurantID, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}

	var req RestaurantCallbackRequest
	if err := core_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}

	callback := service.OrderStatusCallback{
		ExternalOrderID: req.ExternalOrderID,
		Status:          req.Status,
		EventID:         req.EventID,
	}

	if err := h.svc.HandleRestaurantCallback(ctx, restaurantID, callback); err != nil {
		handler.ErrorResponse(err, "failed to process callback")
		return
	}

	handler.NoContentResponse()
}
