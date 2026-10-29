package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestOrderStatusCanTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     OrderStatus
		to       OrderStatus
		expected bool
	}{
		{"pending to confirmed", OrderStatusPending, OrderStatusConfirmed, true},
		{"pending to cancelled", OrderStatusPending, OrderStatusCancelled, true},
		{"pending to preparing", OrderStatusPending, OrderStatusPreparing, false},
		{"confirmed to preparing", OrderStatusConfirmed, OrderStatusPreparing, true},
		{"confirmed to cancelled", OrderStatusConfirmed, OrderStatusCancelled, true},
		{"confirmed to pending", OrderStatusConfirmed, OrderStatusPending, false},
		{"preparing to ready", OrderStatusPreparing, OrderStatusReady, true},
		{"preparing to confirmed", OrderStatusPreparing, OrderStatusConfirmed, false},
		{"ready to delivering", OrderStatusReady, OrderStatusDelivering, true},
		{"ready to preparing", OrderStatusReady, OrderStatusPreparing, false},
		{"delivering to delivered", OrderStatusDelivering, OrderStatusDelivered, true},
		{"delivering to ready", OrderStatusDelivering, OrderStatusReady, false},
		{"delivered to any", OrderStatusDelivered, OrderStatusPreparing, false},
		{"cancelled to any", OrderStatusCancelled, OrderStatusPending, false},
		{"integration_failed to any", OrderStatusIntegrationFailed, OrderStatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanTransition(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestOrderTransitionStatus(t *testing.T) {
	order := &Order{
		ID:     uuid.New(),
		Status: OrderStatusPending,
		StatusHistory: []OrderStatusHistory{
			{Status: OrderStatusPending, Source: "kitchen"},
		},
	}

	err := order.TransitionStatus(OrderStatusConfirmed, "kitchen")
	if err != nil {
		t.Errorf("TransitionStatus failed: %v", err)
	}
	if order.Status != OrderStatusConfirmed {
		t.Errorf("Status should be confirmed, got %s", order.Status)
	}
	if len(order.StatusHistory) != 2 {
		t.Errorf("StatusHistory should have 2 entries, got %d", len(order.StatusHistory))
	}

	err = order.TransitionStatus(OrderStatusPreparing, "restaurant")
	if err != nil {
		t.Errorf("TransitionStatus failed: %v", err)
	}
	if order.Status != OrderStatusPreparing {
		t.Errorf("Status should be preparing, got %s", order.Status)
	}
	if len(order.StatusHistory) != 3 {
		t.Errorf("StatusHistory should have 3 entries, got %d", len(order.StatusHistory))
	}

	err = order.TransitionStatus(OrderStatusPending, "kitchen")
	if err == nil {
		t.Error("Expected error for invalid transition")
	}
	if order.Status != OrderStatusPreparing {
		t.Errorf("Status should remain preparing, got %s", order.Status)
	}
}
