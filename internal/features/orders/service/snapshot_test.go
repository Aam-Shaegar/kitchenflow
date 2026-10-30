package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cart "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	integration "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	menu "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

type mutableMenu struct{ resultMenu }

func TestRetryUsesFrozenPartnerItemsAfterMenuChanges(t *testing.T) {
	rid, itemID := uuid.New(), uuid.New()
	provider := &mutableMenu{resultMenu{item: menu.MenuItem{ID: itemID, RestaurantID: rid, ExternalID: "partner-original", Name: "Original", Price: 123, IsAvailable: true}}}
	repo := &resultRepository{}
	var requests []integration.CreateOrderRequest
	client := resultClient(func(_ context.Context, q integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
		requests = append(requests, q)
		if len(requests) == 1 {
			return nil, errors.New("timeout")
		}
		return &integration.CreateOrderResponse{ExternalOrderID: q.ExternalOrderID, Status: "confirmed"}, nil
	})
	svc := NewOrderService(repo, resultCart{cart: &cart.Cart{ID: uuid.New(), RestaurantID: rid, Items: []cart.CartItem{{MenuItemID: itemID, Quantity: 2}}}}, provider, routingRestaurants{rid: "https://partner.example"}, client)
	o, err := svc.CreateOrder(context.Background(), "user", "Test address", "key")
	if err != nil {
		t.Fatal(err)
	}
	if o.Items[0].ExternalMenuItemID != "partner-original" {
		t.Fatal("snapshot missing")
	}
	provider.item.ExternalID = "partner-replaced"
	provider.item.Name = "Changed"
	provider.item.Price = 999
	if _, err := svc.RetryIntegration(context.Background(), o.ID, "user"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || !reflect.DeepEqual(requests[0], requests[1]) {
		t.Fatalf("retry payload changed: %+v", requests)
	}
}

func TestLegacySnapshotBlocksRetryBeforePreparingOrSending(t *testing.T) {
	repo := &resultRepository{order: &orders.Order{ID: uuid.New(), Status: orders.OrderStatusIntegrationFailed, Items: []orders.OrderItem{{MenuItemID: uuid.New()}}}}
	client := resultClient(func(context.Context, integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
		t.Fatal("sent legacy item without partner ID")
		return nil, nil
	})
	svc := NewOrderService(repo, nil, nil, resultRestaurant{}, client)
	if _, err := svc.RetryIntegration(context.Background(), repo.order.ID, "user"); !errors.Is(err, coreerrors.ErrConflict) {
		t.Fatalf("error: %v", err)
	}
	if repo.prepared {
		t.Fatal("retry was prepared")
	}
}
