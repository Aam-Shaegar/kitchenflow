package service

import (
	"context"
	"errors"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cart "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	integration "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	menu "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	repo "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	restaurants "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/google/uuid"
	"strings"
	"testing"
)

type duplicateRepo struct {
	repo.OrderRepository
	order *orders.Order
}

func (r duplicateRepo) GetByUserIDAndIdempotencyKey(context.Context, string, string) (*orders.Order, error) {
	return r.order, nil
}
func TestCheckoutRejectsInvalidInputBeforeDependencies(t *testing.T) {
	svc := NewOrderService(nil, nil, nil, nil, nil)
	for _, tc := range []struct{ user, address, key string }{{"", "Address", "key"}, {"user", "x", "key"}, {"user", "Address", ""}, {"user", "Address", strings.Repeat("x", 129)}} {
		if _, err := svc.CreateOrder(context.Background(), tc.user, tc.address, tc.key); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatalf("invalid input: %v", err)
		}
	}
}
func TestDuplicateCheckoutDoesNotReadCartOrSendAgain(t *testing.T) {
	original := &orders.Order{ID: uuid.New(), Status: orders.OrderStatusConfirmed}
	svc := NewOrderService(duplicateRepo{order: original}, nil, nil, nil, nil)
	got, err := svc.CreateOrder(context.Background(), "user", "Address", "key")
	if err != nil || got.ID != original.ID {
		t.Fatalf("duplicate result %v %v", got, err)
	}
}
func TestCheckoutComputesAndFreezesMoney(t *testing.T) {
	rid, itemID := uuid.New(), uuid.New()
	repository := &resultRepository{}
	client := resultClient(func(_ context.Context, q integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
		if len(q.Items) != 1 || q.Items[0].UnitPrice != 54900 || q.Items[0].Quantity != 2 {
			t.Fatalf("wrong partner price snapshot %+v", q.Items)
		}
		return &integration.CreateOrderResponse{ExternalOrderID: q.ExternalOrderID, Status: "confirmed"}, nil
	})
	svc := NewOrderService(repository, resultCart{cart: &cart.Cart{ID: uuid.New(), RestaurantID: rid, Items: []cart.CartItem{{MenuItemID: itemID, Quantity: 2}}}}, resultMenu{item: menu.MenuItem{ID: itemID, RestaurantID: rid, ExternalID: "partner-sku", Name: "Dish", Price: 54900, IsAvailable: true}}, resultRestaurant{restaurant: restaurants.RestaurantInfo{IsOpen: true}}, client)
	got, err := svc.CreateOrder(context.Background(), "user", "Address", "money")
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalAmount != 109800 || got.Items[0].TotalPrice != 109800 || got.Items[0].UnitPrice != 54900 {
		t.Fatalf("incorrect order totals %+v", got)
	}
}
func TestCallbackInputValidation(t *testing.T) {
	svc := NewOrderService(nil, nil, nil, nil, nil)
	for _, q := range []OrderStatusCallback{{Status: "confirmed", EventID: "event"}, {ExternalOrderID: "order", Status: "confirmed"}, {ExternalOrderID: "order", Status: "shipped", EventID: "event"}} {
		if err := svc.HandleRestaurantCallback(context.Background(), uuid.New(), q); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatalf("callback validation %v", err)
		}
	}
}

func TestCheckoutBusinessGuardsDoNotPersistOrDispatch(t *testing.T) {
	for _, name := range []string{"empty cart", "missing item", "unavailable item", "foreign item", "missing partner ID", "invalid quantity", "closed restaurant"} {
		t.Run(name, func(t *testing.T) {
			rid, itemID := uuid.New(), uuid.New()
			repository := &resultRepository{}
			c := &cart.Cart{ID: uuid.New(), RestaurantID: rid, Items: []cart.CartItem{{MenuItemID: itemID, Quantity: 1}}}
			m := menu.MenuItem{ID: itemID, RestaurantID: rid, ExternalID: "partner", IsAvailable: true, Price: 100}
			restaurant := restaurants.RestaurantInfo{ID: rid, IsOpen: true}
			want := coreerrors.ErrConflict
			switch name {
			case "empty cart":
				c.Items = nil
			case "missing item":
				m.ID = uuid.New()
				want = coreerrors.ErrNotFound
			case "unavailable item":
				m.IsAvailable = false
			case "foreign item":
				m.RestaurantID = uuid.New()
			case "missing partner ID":
				m.ExternalID = ""
			case "invalid quantity":
				c.Items[0].Quantity = 0
				want = coreerrors.ErrInvalidArgument
			case "closed restaurant":
				restaurant.IsOpen = false
			}
			client := resultClient(func(context.Context, integration.CreateOrderRequest) (*integration.CreateOrderResponse, error) {
				t.Fatal("invalid checkout contacted restaurant")
				return nil, nil
			})
			svc := NewOrderService(repository, resultCart{cart: c}, resultMenu{item: m}, resultRestaurant{restaurant: restaurant}, client)
			if _, err := svc.CreateOrder(context.Background(), "user", "Address", "key"); !errors.Is(err, want) {
				t.Fatalf("error %v want %v", err, want)
			}
			if repository.order != nil {
				t.Fatal("invalid checkout persisted an order")
			}
		})
	}
}
