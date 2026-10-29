package domain

import (
	"context"
)

type RestaurantClient interface {
	CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResponse, error)
}

type CreateOrderRequest struct {
	ExternalOrderID string            `json:"external_order_id"`
	Items           []CreateOrderItem `json:"items"`
	DeliveryAddress string            `json:"delivery_address"`
}

type CreateOrderItem struct {
	ExternalMenuItemID string `json:"external_menu_item_id"`
	Name               string `json:"name"`
	// UnitPrice is kept in minor units (kopecks) as int64, never float.
	UnitPrice int64 `json:"unit_price"`
	Quantity  int   `json:"quantity"`
}

type CreateOrderResponse struct {
	ExternalOrderID string `json:"external_order_id"`
	Status          string `json:"status"`
}
