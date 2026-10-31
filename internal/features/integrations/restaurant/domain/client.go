package domain

import (
	"context"
)

// RestaurantClient implementations must echo the request ExternalOrderID in
// responses and callbacks. Creation responses use pending, confirmed or cancelled.
type RestaurantClient interface {
	ValidateEndpoint(baseURL string) error
	CancelOrder(ctx context.Context, baseURL, externalOrderID string) (*CreateOrderResponse, error)
	CreateOrder(ctx context.Context, baseURL string, req CreateOrderRequest) (*CreateOrderResponse, error)
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
