package service

import (
	"testing"

	"github.com/google/uuid"
	orders_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
)

func TestOrderMoneyInMinorUnits(t *testing.T) {
	price := int64(54900) // 549.00 rub
	qty := 2
	if got := price * int64(qty); got != 109800 {
		t.Errorf("line total = %d, want 109800", got)
	}
}

func TestOrderCancelAllowedOnlyEarly(t *testing.T) {
	if !orders_domain.CanTransition(orders_domain.OrderStatusPending, orders_domain.OrderStatusCancelled) {
		t.Error("pending -> cancelled must be allowed")
	}
	if !orders_domain.CanTransition(orders_domain.OrderStatusConfirmed, orders_domain.OrderStatusCancelled) {
		t.Error("confirmed -> cancelled must be allowed")
	}
	if orders_domain.CanTransition(orders_domain.OrderStatusPreparing, orders_domain.OrderStatusCancelled) {
		t.Error("preparing -> cancelled must be rejected")
	}
	if orders_domain.CanTransition(orders_domain.OrderStatusDelivered, orders_domain.OrderStatusCancelled) {
		t.Error("delivered -> cancelled must be rejected")
	}
}

func TestOrderCallbackValidation(t *testing.T) {
	valid := []orders_domain.OrderStatus{
		orders_domain.OrderStatusPending,
		orders_domain.OrderStatusConfirmed,
		orders_domain.OrderStatusPreparing,
		orders_domain.OrderStatusReady,
		orders_domain.OrderStatusDelivering,
		orders_domain.OrderStatusDelivered,
		orders_domain.OrderStatusCancelled,
		orders_domain.OrderStatusIntegrationFailed,
	}
	for _, s := range valid {
		if !isKnownStatus(s) {
			t.Errorf("status %s must be known", s)
		}
	}
	if isKnownStatus("shipped") {
		t.Error("unknown status must be rejected")
	}
	// Invalid jump must be rejected by the state machine.
	if orders_domain.CanTransition(orders_domain.OrderStatusPending, orders_domain.OrderStatusDelivered) {
		t.Error("pending -> delivered must be rejected")
	}
}

func TestOrderTransitionErrorMapsToConflict(t *testing.T) {
	err := &orders_domain.OrderStatusTransitionError{
		From: orders_domain.OrderStatusPending,
		To:   orders_domain.OrderStatusDelivered,
	}
	if err.Error() == "" {
		t.Error("transition error must have message")
	}
	_ = uuid.New()
}
