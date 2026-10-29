package service

import (
	"context"
	"errors"
	"testing"

	d "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	r "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	"github.com/google/uuid"
)

type callbackRepository struct {
	r.OrderRepository
	order  *d.Order
	err    error
	called bool
}

func (r *callbackRepository) GetByExternalOrderIDAndRestaurantID(context.Context, string, uuid.UUID) (*d.Order, error) {
	return r.order, nil
}
func (r *callbackRepository) UpdateStatusWithHistory(_ context.Context, id uuid.UUID, status d.OrderStatus, h *d.OrderStatusHistory) error {
	r.called = true
	if id != r.order.ID || h.OrderID != id || h.Status != status || h.EventID == nil || *h.EventID != "event" || h.Source != "restaurant" {
		panic("incorrect callback persistence arguments")
	}
	return r.err
}
func TestCallbackDefersReplayAndTransitionValidationToRepository(t *testing.T) {
	for _, status := range []d.OrderStatus{d.OrderStatusPreparing, d.OrderStatusConfirmed} {
		t.Run(string(status), func(t *testing.T) {
			repo := &callbackRepository{order: &d.Order{ID: uuid.New(), Status: status}}
			svc := NewOrderService(repo, nil, nil, nil, nil)
			if err := svc.HandleRestaurantCallback(context.Background(), uuid.New(), OrderStatusCallback{ExternalOrderID: "external", Status: "confirmed", EventID: "event"}); err != nil {
				t.Fatal(err)
			}
			if !repo.called {
				t.Fatal("callback bypassed atomic persistence")
			}
		})
	}
}
func TestCallbackPropagatesPersistenceFailure(t *testing.T) {
	failure := errors.New("database unavailable")
	repo := &callbackRepository{order: &d.Order{ID: uuid.New(), Status: d.OrderStatusConfirmed}, err: failure}
	svc := NewOrderService(repo, nil, nil, nil, nil)
	if err := svc.HandleRestaurantCallback(context.Background(), uuid.New(), OrderStatusCallback{ExternalOrderID: "external", Status: "confirmed", EventID: "event"}); !errors.Is(err, failure) {
		t.Fatalf("got %v, want persistence error", err)
	}
}
