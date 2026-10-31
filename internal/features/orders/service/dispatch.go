package service

import (
	"context"
	"fmt"
	integration "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

type DispatchRepository interface {
	ClaimDispatch(context.Context) (uuid.UUID, uuid.UUID, string, error)
	ReleaseDispatch(context.Context, uuid.UUID, uuid.UUID) error
}

func (s *OrderService) RecoverDispatch(ctx context.Context, jobs DispatchRepository) error {
	id, token, base, err := jobs.ClaimDispatch(ctx)
	if err != nil || id == uuid.Nil {
		return err
	}
	o, err := s.repo.GetByID(ctx, id)
	if err == nil && o != nil && o.ExternalOrderID != nil {
		if o.Status != orders.OrderStatusPending && o.Status != orders.OrderStatusIntegrationFailed {
			err = s.repo.SaveIntegrationResult(ctx, id, *o.ExternalOrderID, orders.OrderStatusConfirmed)
		} else {
			req := integration.CreateOrderRequest{ExternalOrderID: *o.ExternalOrderID, DeliveryAddress: o.DeliveryAddress}
			for _, item := range o.Items {
				if item.ExternalMenuItemID == "" {
					err = fmt.Errorf("missing partner item snapshot")
					break
				}
				req.Items = append(req.Items, integration.CreateOrderItem{ExternalMenuItemID: item.ExternalMenuItemID, Name: item.Name, UnitPrice: item.UnitPrice, Quantity: item.Quantity})
			}
			if err == nil {
				err = s.sendOrder(ctx, base, id, req)
			}
		}
	}
	releaseErr := jobs.ReleaseDispatch(ctx, id, token)
	if err != nil {
		return err
	}
	return releaseErr
}
