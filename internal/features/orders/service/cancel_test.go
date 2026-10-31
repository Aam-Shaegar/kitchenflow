package service

import (
	"context"
	"errors"
	"testing"

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	integration "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

type cancelClient struct {
	resultClient
	cancel func() (*integration.CreateOrderResponse, error)
}

func (c cancelClient) CancelOrder(context.Context, string, string) (*integration.CreateOrderResponse, error) {
	return c.cancel()
}

type cancelRepo struct {
	resultRepository
	writes int
}

func (r *cancelRepo) UpdateStatusWithHistory(_ context.Context, _ uuid.UUID, status orders.OrderStatus, h *orders.OrderStatusHistory) error {
	if !orders.CanTransition(r.order.Status, status) && r.order.Status != status {
		return coreerrors.ErrConflict
	}
	r.writes++
	r.order.Status = status
	return nil
}
func TestCancellationRequiresPartnerConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		status  string
		advance orders.OrderStatus
		want    error
		writes  int
	}{
		{name: "accepted", status: "cancelled", writes: 1},
		{name: "refused", err: coreerrors.ErrConflict, want: coreerrors.ErrConflict},
		{name: "timeout", err: context.DeadlineExceeded, want: coreerrors.ErrIntegrationUnavailable},
		{name: "invalid response", status: "confirmed", want: coreerrors.ErrIntegrationUnavailable},
		{name: "early cancelled callback", err: context.DeadlineExceeded, advance: orders.OrderStatusCancelled},
		{name: "cooking wins", err: coreerrors.ErrConflict, advance: orders.OrderStatusPreparing, want: coreerrors.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			r := &cancelRepo{resultRepository: resultRepository{order: &orders.Order{ID: uuid.New(), ExternalOrderID: &id, Status: orders.OrderStatusConfirmed}}}
			c := cancelClient{cancel: func() (*integration.CreateOrderResponse, error) {
				if tc.advance != "" {
					r.order.Status = tc.advance
				}
				return &integration.CreateOrderResponse{ExternalOrderID: id, Status: tc.status}, tc.err
			}}
			svc := NewOrderService(r, nil, nil, resultRestaurant{}, c)
			_, err := svc.CancelOrder(context.Background(), r.order.ID, "user")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v want %v", err, tc.want)
			}
			if r.writes != tc.writes {
				t.Fatalf("writes=%d", r.writes)
			}
			if tc.want != nil && r.order.Status == orders.OrderStatusCancelled {
				t.Fatal("falsely reported cancellation")
			}
			if tc.want == nil {
				if r.order.Status != orders.OrderStatusCancelled {
					t.Fatal("not cancelled")
				}
				c.cancel = func() (*integration.CreateOrderResponse, error) { t.Fatal("repeat contacted partner"); return nil, nil }
				if _, err := svc.CancelOrder(context.Background(), r.order.ID, "user"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
