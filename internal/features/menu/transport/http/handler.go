package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	core_errors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	core_http_request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	core_http_response "github.com/Aam-Shaegar/kitchenflow/internal/core/http/response"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/service"
)

type MenuHandler struct {
	svc *service.MenuService
}

func NewMenuHandler(svc *service.MenuService) *MenuHandler {
	return &MenuHandler{svc: svc}
}

func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}

	availableOnly := r.URL.Query().Get("available") == "true"
	menu, err := h.svc.GetMenuByRestaurantID(ctx, id)
	if err != nil {
		handler.ErrorResponse(err, "failed to get menu")
		return
	}
	if availableOnly {
		for i := range menu.Categories {
			filtered := menu.Categories[i].Items[:0]
			for _, item := range menu.Categories[i].Items {
				if item.IsAvailable {
					filtered = append(filtered, item)
				}
			}
			menu.Categories[i].Items = filtered
		}
	}

	handler.JSONResponse(menu, http.StatusOK)
}

func (h *MenuHandler) GetMenuItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := core_http_request.ParseUUID(r.PathValue("menu_item_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid menu item ID")
		return
	}

	item, err := h.svc.GetItemByID(ctx, id)
	if err != nil {
		handler.ErrorResponse(err, "failed to get menu item")
		return
	}

	handler.JSONResponse(item, http.StatusOK)
}

type CreateCategoryRequest struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
}

func (h *MenuHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	restaurantID, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}
	var req CreateCategoryRequest
	if err := decodeBody(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}
	cat, err := h.svc.CreateCategory(ctx, restaurantID, req.Name, req.Position)
	if err != nil {
		handler.ErrorResponse(err, "failed to create category")
		return
	}
	handler.JSONResponse(cat, http.StatusCreated)
}

type CreateMenuItemRequest struct {
	CategoryID  string `json:"category_id"`
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       int64  `json:"price"`
}

func (h *MenuHandler) CreateItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	restaurantID, err := core_http_request.ParseUUID(r.PathValue("restaurant_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid restaurant ID")
		return
	}
	var req CreateMenuItemRequest
	if err := decodeBody(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}
	categoryID, err := core_http_request.ParseUUID(req.CategoryID)
	if err != nil {
		handler.ErrorResponse(err, "invalid category ID")
		return
	}
	item, err := h.svc.CreateItem(ctx, service.CreateItemInput{
		RestaurantID: restaurantID,
		CategoryID:   categoryID,
		ExternalID:   req.ExternalID,
		Name:         req.Name,
		Description:  req.Description,
		Price:        req.Price,
	})
	if err != nil {
		handler.ErrorResponse(err, "failed to create menu item")
		return
	}
	handler.JSONResponse(item, http.StatusCreated)
}

type UpdateMenuItemRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Price       *int64  `json:"price"`
	IsAvailable *bool   `json:"is_available"`
	CategoryID  *string `json:"category_id"`
}

func (h *MenuHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := core_http_request.ParseUUID(r.PathValue("menu_item_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid menu item ID")
		return
	}
	var req UpdateMenuItemRequest
	if err := decodeBody(r, &req); err != nil {
		handler.ErrorResponse(err, "invalid request")
		return
	}
	var catID *uuid.UUID
	if req.CategoryID != nil {
		parsed, err := core_http_request.ParseUUID(*req.CategoryID)
		if err != nil {
			handler.ErrorResponse(err, "invalid category ID")
			return
		}
		catID = &parsed
	}
	item, err := h.svc.UpdateItem(ctx, id, service.UpdateItemInput{
		Name:        req.Name,
		Description: req.Description,
		Price:       req.Price,
		IsAvailable: req.IsAvailable,
		CategoryID:  catID,
	})
	if err != nil {
		handler.ErrorResponse(err, "failed to update menu item")
		return
	}
	handler.JSONResponse(item, http.StatusOK)
}

func (h *MenuHandler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	handler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := core_http_request.ParseUUID(r.PathValue("menu_item_id"))
	if err != nil {
		handler.ErrorResponse(err, "invalid menu item ID")
		return
	}
	if err := h.svc.DeleteItem(ctx, id); err != nil {
		handler.ErrorResponse(err, "failed to delete menu item")
		return
	}
	handler.NoContentResponse()
}

// decodeBody reads a JSON body. Any failure is the client's fault,
// so we label it invalid input and the API answers 400, never 500.
func decodeBody(r *http.Request, dest any) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return core_errors.ErrInvalidArgument
	}
	return nil
}
