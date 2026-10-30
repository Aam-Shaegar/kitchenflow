package service

import (
	"context"
	"errors"
	"testing"

	cart "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	cartrepo "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	integration "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	menu "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	orderrepo "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	restaurants "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/google/uuid"
)

type resultRepository struct {
	orderrepo.OrderRepository
	order    *orders.Order
	result   orders.OrderStatus
	cartID   *uuid.UUID
	prepared bool
}

func (r *resultRepository) GetByUserIDAndIdempotencyKey(context.Context, string, string) (*orders.Order, error) {
	return nil, nil
}
func (r *resultRepository) CreateWithIdempotencyKey(_ context.Context, o *orders.Order) (*orders.Order, error) {
	r.order = o
	return o, nil
}
func (r *resultRepository) GetByIDAndUserID(context.Context, uuid.UUID, string) (*orders.Order, error) {
	return r.order, nil
}
func (r *resultRepository) SaveIntegrationResult(_ context.Context, _ uuid.UUID, externalID string, status orders.OrderStatus, cartID *uuid.UUID) error {
	if r.order.ExternalOrderID == nil || *r.order.ExternalOrderID != externalID {
		panic("correlation ID was not persisted before sending")
	}
	r.result = status
	r.cartID = cartID
	r.order.Status = status
	return nil
}
func (r *resultRepository) PrepareIntegrationRetry(_ context.Context, _ uuid.UUID, externalID string) error {
	r.prepared = true
	r.order.Status = orders.OrderStatusPending
	r.order.ExternalOrderID = &externalID
	return nil
}

type resultCart struct {
	cartrepo.CartRepository
	cart *cart.Cart
}

func (c resultCart) GetByUserID(context.Context, string) (*cart.Cart, error) { return c.cart, nil }

type resultMenu struct{ item menu.MenuItem }

func (m resultMenu) GetItemsByIDs(context.Context, []uuid.UUID) ([]menu.MenuItem, error) {
	return []menu.MenuItem{m.item}, nil
}

type resultRestaurant struct{ restaurant restaurants.RestaurantInfo }

func (r resultRestaurant) GetByIDForOrder(context.Context, uuid.UUID) (*restaurants.RestaurantInfo, error) {
	return &r.restaurant, nil
}

type resultClient func(context.Context, integration.CreateOrderRequest) (*integration.CreateOrderResponse, error)

func (c resultClient) ValidateEndpoint(string) error { return nil }

func (c resultClient) CreateOrder(ctx context.Context, _ string, q integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
	return c(ctx, q)
}

func TestCreateOrderPersistsCorrelationAndUsesRestaurantResult(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		badID, fail  bool
		want         orders.OrderStatus
	}{
		{name: "manual acceptance", status: "confirmed", want: orders.OrderStatusConfirmed},
		{name: "rejection", status: "cancelled", want: orders.OrderStatusCancelled},
		{name: "pending", status: "pending", want: orders.OrderStatusPending},
		{name: "invalid status", status: "delivered", want: orders.OrderStatusIntegrationFailed},
		{name: "wrong correlation", status: "confirmed", badID: true, want: orders.OrderStatusIntegrationFailed},
		{name: "timeout", fail: true, want: orders.OrderStatusIntegrationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rid, itemID := uuid.New(), uuid.New()
			repo := &resultRepository{}
			c := &cart.Cart{ID: uuid.New(), RestaurantID: rid, Items: []cart.CartItem{{MenuItemID: itemID, Quantity: 1}}}
			client := resultClient(func(_ context.Context, q integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
				if repo.order == nil || repo.order.ExternalOrderID == nil || *repo.order.ExternalOrderID != q.ExternalOrderID {
					t.Fatal("early callback cannot find persisted correlation")
				}
				if tc.fail {
					return nil, errors.New("timeout")
				}
				id := q.ExternalOrderID
				if tc.badID {
					id = "wrong-id"
				}
				return &integration.CreateOrderResponse{ExternalOrderID: id, Status: tc.status}, nil
			})
			svc := NewOrderService(repo, resultCart{cart: c}, resultMenu{item: menu.MenuItem{ID: itemID, RestaurantID: rid, IsAvailable: true, Price: 100}}, resultRestaurant{restaurant: restaurants.RestaurantInfo{ID: rid, IsOpen: true}}, client)
			got, err := svc.CreateOrder(context.Background(), "user", "Test address", "key")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want || repo.result != tc.want {
				t.Fatalf("status=%s want=%s", got.Status, tc.want)
			}
			if repo.cartID == nil || *repo.cartID != c.ID {
				t.Fatal("original cart ID missing from atomic reconciliation")
			}
		})
	}
}
func TestRetryPreparesCorrelationBeforeRestaurantCall(t *testing.T) {
	repo := &resultRepository{order: &orders.Order{ID: uuid.New(), Status: orders.OrderStatusIntegrationFailed}}
	client := resultClient(func(_ context.Context, q integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
		if !repo.prepared || repo.order.Status != orders.OrderStatusPending {
			t.Fatal("retry not prepared before callback")
		}
		return &integration.CreateOrderResponse{ExternalOrderID: q.ExternalOrderID, Status: "cancelled"}, nil
	})
	svc := NewOrderService(repo, nil, nil, resultRestaurant{}, client)
	got, err := svc.RetryIntegration(context.Background(), repo.order.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != orders.OrderStatusCancelled {
		t.Fatalf("status=%s", got.Status)
	}
}
