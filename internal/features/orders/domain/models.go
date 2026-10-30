package domain

import (
	"time"

	"github.com/google/uuid"
)

type OrderStatus string

const (
	OrderStatusPending           OrderStatus = "pending"
	OrderStatusConfirmed         OrderStatus = "confirmed"
	OrderStatusPreparing         OrderStatus = "preparing"
	OrderStatusReady             OrderStatus = "ready"
	OrderStatusDelivering        OrderStatus = "delivering"
	OrderStatusDelivered         OrderStatus = "delivered"
	OrderStatusCancelled         OrderStatus = "cancelled"
	OrderStatusIntegrationFailed OrderStatus = "integration_failed"
)

type Order struct {
	// CheckoutCartID is a transient command input, consumed atomically on creation.
	CheckoutCartID  *uuid.UUID  `json:"-"`
	ID              uuid.UUID   `json:"id"`
	UserID          string      `json:"user_id"`
	RestaurantID    uuid.UUID   `json:"restaurant_id"`
	ExternalOrderID *string     `json:"external_order_id,omitempty"`
	Status          OrderStatus `json:"status"`
	// TotalAmount is kept in minor units (kopecks) as int64, never float.
	TotalAmount     int64                `json:"total_amount"`
	DeliveryAddress string               `json:"delivery_address"`
	IdempotencyKey  string               `json:"idempotency_key"`
	Items           []OrderItem          `json:"items"`
	StatusHistory   []OrderStatusHistory `json:"status_history"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

type OrderItem struct {
	ExternalMenuItemID string    `json:"external_menu_item_id,omitempty"`
	ID                 uuid.UUID `json:"id"`
	OrderID            uuid.UUID `json:"order_id"`
	MenuItemID         uuid.UUID `json:"menu_item_id"`
	Name               string    `json:"name"`
	// UnitPrice is a frozen snapshot in minor units (kopecks); history must survive menu edits.
	UnitPrice int64 `json:"unit_price"`
	Quantity  int   `json:"quantity"`
	// TotalPrice is kept in minor units (kopecks) as int64, never float.
	TotalPrice int64     `json:"total_price"`
	CreatedAt  time.Time `json:"created_at"`
}

type OrderStatusHistory struct {
	ID        int64       `json:"id"`
	OrderID   uuid.UUID   `json:"order_id"`
	Status    OrderStatus `json:"status"`
	Source    string      `json:"source"`
	EventID   *string     `json:"event_id,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type OrderStatusTransitionError struct {
	From OrderStatus
	To   OrderStatus
}

func (e *OrderStatusTransitionError) Error() string {
	return "invalid order status transition: " + string(e.From) + " -> " + string(e.To)
}

func CanTransition(from, to OrderStatus) bool {
	transitions := map[OrderStatus][]OrderStatus{
		OrderStatusPending:           {OrderStatusConfirmed, OrderStatusCancelled},
		OrderStatusConfirmed:         {OrderStatusPreparing, OrderStatusCancelled},
		OrderStatusPreparing:         {OrderStatusReady},
		OrderStatusReady:             {OrderStatusDelivering},
		OrderStatusDelivering:        {OrderStatusDelivered},
		OrderStatusDelivered:         {},
		OrderStatusCancelled:         {},
		OrderStatusIntegrationFailed: {},
	}

	allowed, exists := transitions[from]
	if !exists {
		return false
	}

	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

func (o *Order) TransitionStatus(to OrderStatus, source string) error {
	if !CanTransition(o.Status, to) {
		return &OrderStatusTransitionError{From: o.Status, To: to}
	}
	o.Status = to
	o.UpdatedAt = time.Now()
	o.StatusHistory = append(o.StatusHistory, OrderStatusHistory{
		OrderID:   o.ID,
		Status:    to,
		Source:    source,
		CreatedAt: time.Now(),
	})
	return nil
}
