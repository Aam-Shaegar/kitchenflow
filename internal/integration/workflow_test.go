package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Aam-Shaegar/kitchenflow/internal/core/callbackauth"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/database/migrate"
	pg "github.com/Aam-Shaegar/kitchenflow/internal/core/database/pgx"
	auth "github.com/Aam-Shaegar/kitchenflow/internal/core/http/middleware"
	logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	cartrepo "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	cartsvc "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/service"
	carthttp "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/transport/http"
	partner "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	partnerhttp "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/http"
	callbackhttp "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/transport/http"
	menurepo "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/repository"
	menusvc "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/service"
	menuhttp "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/transport/http"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	orderrepo "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	ordersvc "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/service"
	orderhttp "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/transport/http"
	restrepo "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/repository"
	restsvc "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/service"
	resthttp "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/transport/http"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func TestAuthenticatedHTTPWorkflowPostgres(t *testing.T) {
	raw := os.Getenv("KITCHENFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set disposable PostgreSQL URL; scripts/test-integration.sh requires it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	adminDB, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	schema := "workflow_" + uuid.NewString()[:8]
	if _, err := adminDB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := adminDB.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, err := pg.NewConnectionPool(pg.Config{URL: u.String(), Timeout: 5 * time.Second}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrate.Run(ctx, pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	rid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	write := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tokens := map[string]string{}
	roles := []string{"alice", "bob", "partner", "admin"}
	credentials := make([]map[string]any, 0, len(roles))
	for _, role := range roles {
		token := uuid.NewString() + uuid.NewString()
		tokens[role] = token
		hash := sha256.Sum256([]byte(token))
		kind := role
		if role == "alice" || role == "bob" {
			kind = "user"
		}
		credentials = append(credentials, map[string]any{"subject": role, "role": kind, "restaurant_ids": []uuid.UUID{rid}, "expires_at": time.Now().Add(time.Hour), "token_sha256": hex.EncodeToString(hash[:])})
	}
	identity, err := auth.LoadAuth(write("auth.json", credentials))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := callbackauth.Load(write("keys.json", []map[string]any{{"key_id": "test", "restaurant_id": rid, "expires_at": time.Now().Add(time.Hour), "secret_base64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))}}))
	if err != nil {
		t.Fatal(err)
	}
	sign, err := keys.Signer("test", rid)
	if err != nil {
		t.Fatal(err)
	}
	var unavailable atomic.Bool
	var creates, cancels atomic.Int32
	requests := make(chan partner.CreateOrderRequest, 10)
	restaurant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request partner.CreateOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		status := "confirmed"
		if r.URL.Path == "/api/v1/restaurant/orders/cancel" {
			cancels.Add(1)
			status = "cancelled"
		} else {
			creates.Add(1)
			requests <- request
			if unavailable.Load() {
				w.WriteHeader(503)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(partner.CreateOrderResponse{ExternalOrderID: request.ExternalOrderID, Status: status})
	}))
	defer restaurant.Close()
	client, err := partnerhttp.NewHTTPClient(time.Second, []string{restaurant.URL}, []string{"127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	restaurants := restsvc.NewRestaurantService(restrepo.NewRestaurantRepository(pool))
	menu := menusvc.NewMenuService(menurepo.NewMenuRepository(pool))
	cart := cartsvc.NewCartService(cartrepo.NewCartRepository(pool), menu)
	order := ordersvc.NewOrderService(orderrepo.NewOrderRepository(pool), cartrepo.NewCartRepository(pool), menu, restaurants, client)
	rh, mh, ch, oh, cb := resthttp.NewRestaurantHandler(restaurants), menuhttp.NewMenuHandler(menu), carthttp.NewCartHandler(cart), orderhttp.NewOrderHandler(order), callbackhttp.NewIntegrationHandler(order)
	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc, guards ...auth.Middleware) {
		var handler http.Handler = h
		for i := len(guards) - 1; i >= 0; i-- {
			handler = guards[i](handler)
		}
		mux.Handle(pattern, handler)
	}
	scope := identity.Restaurant(auth.RestaurantPath)
	itemScope := identity.Restaurant(func(r *http.Request) (uuid.UUID, error) {
		id, err := uuid.Parse(r.PathValue("menu_item_id"))
		if err != nil {
			return uuid.Nil, err
		}
		item, err := menu.GetItemByID(r.Context(), id)
		if err != nil {
			return uuid.Nil, err
		}
		return item.RestaurantID, nil
	})
	route("POST /api/v1/restaurants", rh.CreateRestaurant, identity.Admin())
	route("GET /api/v1/restaurants", rh.ListRestaurants)
	route("PATCH /api/v1/restaurants/{restaurant_id}", rh.UpdateRestaurant, scope)
	route("GET /api/v1/restaurants/{restaurant_id}/menu", mh.GetMenu)
	route("POST /api/v1/restaurants/{restaurant_id}/menu/categories", mh.CreateCategory, scope)
	route("POST /api/v1/restaurants/{restaurant_id}/menu/items", mh.CreateItem, scope)
	route("PATCH /api/v1/menu-items/{menu_item_id}", mh.UpdateItem, itemScope)
	route("DELETE /api/v1/menu-items/{menu_item_id}", mh.DeleteItem, itemScope)
	route("GET /api/v1/cart", ch.GetCart, identity.User())
	route("POST /api/v1/cart/items", ch.AddItem, identity.User())
	route("PATCH /api/v1/cart/items/{cart_item_id}", ch.UpdateItem, identity.User())
	route("DELETE /api/v1/cart/items/{cart_item_id}", ch.RemoveItem, identity.User())
	route("DELETE /api/v1/cart", ch.ClearCart, identity.User())
	route("POST /api/v1/orders", oh.CreateOrder, identity.User())
	route("GET /api/v1/orders", oh.GetOrders, identity.User())
	route("GET /api/v1/orders/{order_id}", oh.GetOrder, identity.User())
	route("GET /api/v1/orders/{order_id}/status", oh.GetOrderStatus, identity.User())
	route("GET /api/v1/orders/{order_id}/history", oh.GetOrderHistory, identity.User())
	route("POST /api/v1/orders/{order_id}/cancel", oh.CancelOrder, identity.User())
	route("POST /api/v1/orders/{order_id}/retry", oh.RetryOrder, identity.User())
	route("GET /api/v1/restaurants/{restaurant_id}/orders", oh.ListRestaurantOrders, scope)
	route("POST /api/v1/integrations/restaurants/{restaurant_id}/orders/status", cb.RestaurantCallback, keys.Middleware)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(logger.ToContext(r.Context(), &logger.Logger{Logger: zap.NewNop()})))
	}))
	defer api.Close()
	call := func(method, path string, body any, role string, want int, signed bool, key string) json.RawMessage {
		t.Helper()
		var data []byte
		if body != nil {
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, api.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-User-ID", "bob")
		if role != "" {
			request.Header.Set("Authorization", "Bearer "+tokens[role])
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		if signed {
			if err := sign(request, data); err != nil {
				t.Fatal(err)
			}
		}
		response, err := api.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		value := json.RawMessage(raw)

		if response.StatusCode != want {
			t.Fatalf("%s %s HTTP %d want %d response %s", method, path, response.StatusCode, want, value)
		}
		return value
	}
	object := func(raw json.RawMessage) map[string]any {
		t.Helper()
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	decodeOrder := func(raw json.RawMessage) orders.Order {
		t.Helper()
		var value orders.Order
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	// Anonymous identity headers and customer roles cannot access partner writes.
	call("GET", "/api/v1/cart", nil, "", 401, false, "")
	call("POST", "/api/v1/restaurants", map[string]any{"name": "Other"}, "alice", 403, false, "")
	other := object(call("POST", "/api/v1/restaurants", map[string]any{"name": "Other"}, "admin", 201, false, ""))["id"].(string)
	call("PATCH", "/api/v1/restaurants/"+other, map[string]any{"name": "Stolen"}, "partner", 403, false, "")
	call("PATCH", "/api/v1/restaurants/"+rid.String(), map[string]any{"integration_base_url": restaurant.URL}, "partner", 200, false, "")
	category := object(call("POST", "/api/v1/restaurants/"+rid.String()+"/menu/categories", map[string]any{"name": "Workflow", "position": 10}, "partner", 201, false, ""))["id"].(string)
	item := object(call("POST", "/api/v1/restaurants/"+rid.String()+"/menu/items", map[string]any{"category_id": category, "external_id": "partner-sku", "name": "Dish", "price": 54900}, "partner", 201, false, ""))["id"].(string)
	otherCategory := object(call("POST", "/api/v1/restaurants/"+other+"/menu/categories", map[string]any{"name": "Other"}, "admin", 201, false, ""))["id"].(string)
	otherItem := object(call("POST", "/api/v1/restaurants/"+other+"/menu/items", map[string]any{"category_id": otherCategory, "name": "Other dish", "price": 100}, "admin", 201, false, ""))["id"].(string)
	call("PATCH", "/api/v1/menu-items/"+otherItem, map[string]any{"price": 1}, "partner", 403, false, "")
	call("PATCH", "/api/v1/menu-items/"+item, map[string]any{"category_id": otherCategory}, "partner", 400, false, "")
	call("GET", "/api/v1/restaurants/"+rid.String()+"/menu", nil, "", 200, false, "")
	add := func(quantity int, want int) json.RawMessage {
		return call("POST", "/api/v1/cart/items", map[string]any{"restaurant_id": rid, "menu_item_id": item, "quantity": quantity}, "alice", want, false, "")
	}
	c := object(add(2, 201))
	if c["total_amount"] != float64(109800) {
		t.Fatal("cart money incorrect")
	}
	line := c["items"].([]any)[0].(map[string]any)["id"].(string)
	call("PATCH", "/api/v1/cart/items/"+line, map[string]any{"quantity": 0}, "alice", 400, false, "")
	call("PATCH", "/api/v1/cart/items/"+line, map[string]any{"quantity": 3}, "alice", 200, false, "")
	call("DELETE", "/api/v1/cart/items/"+line, nil, "bob", 404, false, "")
	call("DELETE", "/api/v1/cart/items/"+line, nil, "alice", 204, false, "")
	add(2, 201)
	first := decodeOrder(call("POST", "/api/v1/orders", map[string]any{"delivery_address": "Workflow address"}, "alice", 201, false, "one"))
	if first.TotalAmount != 109800 || first.Items[0].ExternalMenuItemID != "partner-sku" {
		t.Fatal("order snapshot incorrect")
	}
	duplicate := decodeOrder(call("POST", "/api/v1/orders", map[string]any{"delivery_address": "Workflow address"}, "alice", 201, false, "one"))
	if duplicate.ID != first.ID || creates.Load() != 1 {
		t.Fatal("duplicate sent twice")
	}
	call("GET", "/api/v1/orders/"+first.ID.String(), nil, "bob", 404, false, "")
	call("GET", "/api/v1/cart", nil, "alice", 404, false, "")
	call("POST", "/api/v1/orders/"+first.ID.String()+"/cancel", nil, "alice", 200, false, "")
	call("POST", "/api/v1/orders/"+first.ID.String()+"/cancel", nil, "alice", 200, false, "")
	if cancels.Load() != 1 {
		t.Fatal("repeat cancellation sent again")
	}
	call("PATCH", "/api/v1/menu-items/"+item, map[string]any{"price": 60000}, "partner", 200, false, "")
	add(1, 201)
	unavailable.Store(true)
	second := decodeOrder(call("POST", "/api/v1/orders", map[string]any{"delivery_address": "Workflow retry"}, "alice", 201, false, "two"))
	if second.Status != orders.OrderStatusIntegrationFailed {
		t.Fatal("outage not reflected")
	}
	call("PATCH", "/api/v1/menu-items/"+item, map[string]any{"price": 70000}, "partner", 200, false, "")
	unavailable.Store(false)
	retried := decodeOrder(call("POST", "/api/v1/orders/"+second.ID.String()+"/retry", nil, "alice", 200, false, ""))
	if retried.Status != orders.OrderStatusConfirmed {
		t.Fatal("retry not confirmed")
	}
	<-requests
	failed, sent := <-requests, <-requests
	if failed.ExternalOrderID != sent.ExternalOrderID || sent.Items[0].UnitPrice != 60000 || sent.Items[0].ExternalMenuItemID != "partner-sku" {
		t.Fatal("retry used changed menu")
	}
	callbackPath := "/api/v1/integrations/restaurants/" + rid.String() + "/orders/status"
	event := func(status string) map[string]any {
		return map[string]any{"external_order_id": second.ID.String(), "status": status, "event_id": "workflow-" + status}
	}
	call("POST", callbackPath, event("preparing"), "partner", 401, false, "")
	for _, status := range []string{"preparing", "ready", "delivering", "delivered"} {
		call("POST", callbackPath, event(status), "", 204, true, "")
		if status == "preparing" {
			call("POST", "/api/v1/orders/"+second.ID.String()+"/cancel", nil, "alice", 409, false, "")
			if cancels.Load() != 1 {
				t.Fatal("cooking cancellation reached partner")
			}
		}
	}
	before := call("GET", "/api/v1/orders/"+second.ID.String()+"/history", nil, "alice", 200, false, "")
	call("POST", callbackPath, event("preparing"), "", 204, true, "")
	after := call("GET", "/api/v1/orders/"+second.ID.String()+"/history", nil, "alice", 200, false, "")
	if !bytes.Equal(before, after) {
		t.Fatal("callback replay changed history")
	}
	final := decodeOrder(call("GET", "/api/v1/orders/"+second.ID.String(), nil, "alice", 200, false, ""))
	if final.Status != orders.OrderStatusDelivered || final.Items[0].UnitPrice != 60000 {
		t.Fatal("final snapshot incorrect")
	}
	call("GET", "/api/v1/orders/"+second.ID.String()+"/status", nil, "alice", 200, false, "")
	call("GET", "/api/v1/orders", nil, "alice", 200, false, "")
	call("GET", "/api/v1/restaurants/"+rid.String()+"/orders", nil, "partner", 200, false, "")
	call("GET", "/api/v1/restaurants/"+rid.String()+"/orders", nil, "alice", 403, false, "")
	call("DELETE", "/api/v1/menu-items/"+item, nil, "partner", 409, false, "")
	call("DELETE", "/api/v1/cart", nil, "alice", 204, false, "")
}
