package service

import (
	"testing"

	"github.com/google/uuid"
	orders_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
)

func TestOrderStateMachine_Transitions(t *testing.T) {
	tests := []struct {
		name     string
		from     orders_domain.OrderStatus
		to       orders_domain.OrderStatus
		expected bool
	}{
		{"pending -> confirmed", orders_domain.OrderStatusPending, orders_domain.OrderStatusConfirmed, true},
		{"pending -> cancelled", orders_domain.OrderStatusPending, orders_domain.OrderStatusCancelled, true},
		{"pending -> preparing", orders_domain.OrderStatusPending, orders_domain.OrderStatusPreparing, false},
		{"confirmed -> preparing", orders_domain.OrderStatusConfirmed, orders_domain.OrderStatusPreparing, true},
		{"confirmed -> cancelled", orders_domain.OrderStatusConfirmed, orders_domain.OrderStatusCancelled, true},
		{"confirmed -> pending", orders_domain.OrderStatusConfirmed, orders_domain.OrderStatusPending, false},
		{"preparing -> ready", orders_domain.OrderStatusPreparing, orders_domain.OrderStatusReady, true},
		{"preparing -> confirmed", orders_domain.OrderStatusPreparing, orders_domain.OrderStatusConfirmed, false},
		{"ready -> delivering", orders_domain.OrderStatusReady, orders_domain.OrderStatusDelivering, true},
		{"ready -> preparing", orders_domain.OrderStatusReady, orders_domain.OrderStatusPreparing, false},
		{"delivering -> delivered", orders_domain.OrderStatusDelivering, orders_domain.OrderStatusDelivered, true},
		{"delivering -> ready", orders_domain.OrderStatusDelivering, orders_domain.OrderStatusReady, false},
		{"delivered -> any", orders_domain.OrderStatusDelivered, orders_domain.OrderStatusPreparing, false},
		{"cancelled -> any", orders_domain.OrderStatusCancelled, orders_domain.OrderStatusPending, false},
		{"integration_failed -> any", orders_domain.OrderStatusIntegrationFailed, orders_domain.OrderStatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := orders_domain.CanTransition(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestOrder_TransitionStatus(t *testing.T) {
	order := &orders_domain.Order{
		ID:     uuid.New(),
		Status: orders_domain.OrderStatusPending,
		StatusHistory: []orders_domain.OrderStatusHistory{
			{Status: orders_domain.OrderStatusPending, Source: "kitchen"},
		},
	}

	err := order.TransitionStatus(orders_domain.OrderStatusConfirmed, "kitchen")
	if err != nil {
		t.Errorf("TransitionStatus failed: %v", err)
	}
	if order.Status != orders_domain.OrderStatusConfirmed {
		t.Errorf("Status should be confirmed, got %s", order.Status)
	}
	if len(order.StatusHistory) != 2 {
		t.Errorf("StatusHistory should have 2 entries, got %d", len(order.StatusHistory))
	}

	err = order.TransitionStatus(orders_domain.OrderStatusPreparing, "restaurant")
	if err != nil {
		t.Errorf("TransitionStatus failed: %v", err)
	}
	if order.Status != orders_domain.OrderStatusPreparing {
		t.Errorf("Status should be preparing, got %s", order.Status)
	}
	if len(order.StatusHistory) != 3 {
		t.Errorf("StatusHistory should have 3 entries, got %d", len(order.StatusHistory))
	}

	err = order.TransitionStatus(orders_domain.OrderStatusPending, "kitchen")
	if err == nil {
		t.Error("Expected error for invalid transition")
	}
	if order.Status != orders_domain.OrderStatusPreparing {
		t.Errorf("Status should remain preparing, got %s", order.Status)
	}
}
