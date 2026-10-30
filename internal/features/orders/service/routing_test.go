package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cart "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	integration "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	integrationhttp "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/http"
	menu "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	restaurants "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/google/uuid"
)

type routingRestaurants map[uuid.UUID]string

func (r routingRestaurants) GetByIDForOrder(_ context.Context, id uuid.UUID) (*restaurants.RestaurantInfo, error) {
	return &restaurants.RestaurantInfo{ID: id, IsOpen: true, IntegrationBaseURL: r[id]}, nil
}

func TestCheckoutAndRetryRouteToOrderRestaurant(t *testing.T) {
	endpoints := routingRestaurants{}
	expected := make(map[string]int)
	var received [2]int
	var servers [2]*httptest.Server
	var ids [2]uuid.UUID
	for i := range servers {
		index := i
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req integration.CreateOrderRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if expected[req.ExternalOrderID] != index {
				t.Errorf("order sent to wrong restaurant: %s -> %d", req.ExternalOrderID, index)
			}
			received[index]++
			_ = json.NewEncoder(w).Encode(integration.CreateOrderResponse{ExternalOrderID: req.ExternalOrderID, Status: "confirmed"})
		}))
		defer servers[i].Close()
		ids[i] = uuid.New()
		endpoints[ids[i]] = servers[i].URL
	}
	client, err := integrationhttp.NewHTTPClient(time.Second, []string{servers[0].URL, servers[1].URL}, []string{"127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	for i, rid := range ids {
		itemID := uuid.New()
		repo := &routingRepository{resultRepository: resultRepository{}, expected: expected, index: i}
		svc := NewOrderService(repo, resultCart{cart: &cart.Cart{ID: uuid.New(), RestaurantID: rid, Items: []cart.CartItem{{MenuItemID: itemID, Quantity: 1}}}}, resultMenu{item: menu.MenuItem{ExternalID: "partner-item", ID: itemID, RestaurantID: rid, IsAvailable: true}}, endpoints, client)
		order, err := svc.CreateOrder(context.Background(), "user", "Test address", "key")
		if err != nil {
			t.Fatal(err)
		}
		if order.Status != orders.OrderStatusConfirmed {
			t.Fatalf("checkout status: %s", order.Status)
		}
		repo.order.Status = orders.OrderStatusIntegrationFailed
		order, err = svc.RetryIntegration(context.Background(), order.ID, "user")
		if err != nil {
			t.Fatal(err)
		}
		if order.Status != orders.OrderStatusConfirmed {
			t.Fatalf("retry status: %s", order.Status)
		}
	}
	if received != [2]int{2, 2} {
		t.Fatalf("restaurant counts: %v", received)
	}
}

type routingRepository struct {
	resultRepository
	expected map[string]int
	index    int
}

func (r *routingRepository) CreateWithIdempotencyKey(ctx context.Context, o *orders.Order) (*orders.Order, error) {
	r.expected[o.ID.String()] = r.index
	return r.resultRepository.CreateWithIdempotencyKey(ctx, o)
}

func TestUnavailableIntegrationDoesNotCreateOrPrepareOrder(t *testing.T) {
	client, err := integrationhttp.NewHTTPClient(time.Second, []string{"https://approved.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"", "invalid", "http://169.254.169.254"} {
		t.Run(endpoint, func(t *testing.T) {
			rid, itemID := uuid.New(), uuid.New()
			repo := &resultRepository{}
			svc := NewOrderService(repo, resultCart{cart: &cart.Cart{ID: uuid.New(), RestaurantID: rid, Items: []cart.CartItem{{MenuItemID: itemID, Quantity: 1}}}}, resultMenu{item: menu.MenuItem{ExternalID: "partner-item", ID: itemID, RestaurantID: rid, IsAvailable: true}}, routingRestaurants{rid: endpoint}, client)
			if _, err := svc.CreateOrder(context.Background(), "user", "Test address", "key"); !errors.Is(err, coreerrors.ErrIntegrationUnavailable) {
				t.Fatalf("checkout error: %v", err)
			}
			if repo.order != nil {
				t.Fatal("created order without integration")
			}
			repo.order = &orders.Order{ID: uuid.New(), RestaurantID: rid, Status: orders.OrderStatusIntegrationFailed}
			if _, err := svc.RetryIntegration(context.Background(), repo.order.ID, "user"); !errors.Is(err, coreerrors.ErrIntegrationUnavailable) {
				t.Fatalf("retry error: %v", err)
			}
			if repo.prepared || repo.order.Status != orders.OrderStatusIntegrationFailed {
				t.Fatal("failed retry changed order")
			}
		})
	}
}
