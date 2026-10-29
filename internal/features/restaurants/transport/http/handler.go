package handler

import (
	"net/http"
	"strconv"

	core_http_request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	core_http_response "github.com/Aam-Shaegar/kitchenflow/internal/core/http/response"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/service"
)

type RestaurantHandler struct {
	svc *service.RestaurantService
}

func NewRestaurantHandler(svc *service.RestaurantService) *RestaurantHandler {
	return &RestaurantHandler{svc: svc}
}

func (h *RestaurantHandler) ListRestaurants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	onlyOpen := r.URL.Query().Get("open") == "true"
	limit, offset := parsePagination(r)
	restaurants, err := h.svc.ListFilteredPaginated(ctx, onlyOpen, limit, offset)
	if err != nil {
		handler.ErrorResponse(err, "failed to list restaurants")
		return
	}

	handler.JSONResponse(restaurants, http.StatusOK)
}

func (h *RestaurantHandler) GetRestaurant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}

	restaurant, err := h.svc.GetByID(ctx, id)
	if err != nil {
		handler.ErrorResponse(err, "failed to get restaurant")
		return
	}

	handler.JSONResponse(restaurant, http.StatusOK)
}

type CreateRestaurantRequest struct {
	Name               string `json:"name" validate:"required,min=1,max=200"`
	Description        string `json:"description" validate:"max=2000"`
	ExternalID         string `json:"external_id" validate:"max=128"`
	IntegrationBaseURL string `json:"integration_base_url" validate:"max=512"`
}

func (h *RestaurantHandler) CreateRestaurant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	var req CreateRestaurantRequest
	if err := decodeJSON(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}
	if req.Name == "" {
		handler.ErrorResponse(errInvalid(), "invalid request")
		return
	}

	rest, err := h.svc.Create(ctx, service.CreateRestaurantInput{
		Name:               req.Name,
		Description:        req.Description,
		ExternalID:         req.ExternalID,
		IntegrationBaseURL: req.IntegrationBaseURL,
	})
	if err != nil {
		handler.ErrorResponse(err, "failed to create restaurant")
		return
	}
	handler.JSONResponse(rest, http.StatusCreated)
}

type UpdateRestaurantRequest struct {
	Name               *string `json:"name"`
	Description        *string `json:"description"`
	IsOpen             *bool   `json:"is_open"`
	IntegrationBaseURL *string `json:"integration_base_url"`
}

func (h *RestaurantHandler) UpdateRestaurant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}
	var req UpdateRestaurantRequest
	if err := decodeJSON(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}
	rest, err := h.svc.Update(ctx, id, service.UpdateRestaurantInput{
		Name:               req.Name,
		Description:        req.Description,
		IsOpen:             req.IsOpen,
		IntegrationBaseURL: req.IntegrationBaseURL,
	})
	if err != nil {
		handler.ErrorResponse(err, "failed to update restaurant")
		return
	}
	handler.JSONResponse(rest, http.StatusOK)
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
