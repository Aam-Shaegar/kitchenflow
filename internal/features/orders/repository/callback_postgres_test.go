package repository_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database/pgx"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cartdomain "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	cartrepo "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	d "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	repo "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KITCHENFLOW_TEST_DATABASE_URL must point to a disposable PostgreSQL database.
// Each run creates and drops its own schema; no application tables are touched.
func TestPostgresCallbackIdempotency(t *testing.T) {
	raw := os.Getenv("KITCHENFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set KITCHENFLOW_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "callback_test_" + uuid.New().String()[:8]
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	pool, err := db.NewConnectionPool(db.Config{URL: u.String(), Timeout: 5 * time.Second}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migration, err := os.ReadFile("../../../../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	snapshotMigration, err := os.ReadFile("../../../../migrations/005_order_item_external_id.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(snapshotMigration)); err != nil {
		t.Fatal(err)
	}
	revisionMigration, err := os.ReadFile("../../../../migrations/006_cart_revision.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(revisionMigration)); err != nil {
		t.Fatal(err)
	}
	repository := repo.NewOrderRepository(pool)
	newOrder := func() uuid.UUID {
		t.Helper()
		rid, id := uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO restaurants(id,external_id,name,integration_base_url) VALUES($1,$2,'Test','http://restaurant')`, rid, rid.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO orders(id,user_id,restaurant_id,status,total_amount,delivery_address,idempotency_key,external_order_id) VALUES($1,'user',$2,'pending',100,'Test address',$3,$3)`, id, rid, id.String()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	apply := func(id uuid.UUID, status d.OrderStatus, event string) error {
		return repository.UpdateStatusWithHistory(ctx, id, status, &d.OrderStatusHistory{OrderID: id, Status: status, Source: "restaurant", EventID: &event, CreatedAt: time.Now().UTC()})
	}
	assertState := func(id uuid.UUID, status d.OrderStatus, count int) {
		t.Helper()
		order, err := repository.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if order.Status != status || len(order.StatusHistory) != count {
			t.Fatalf("status/history: %s/%d, want %s/%d", order.Status, len(order.StatusHistory), status, count)
		}
	}
	t.Run("partner item snapshot persists across menu edits", func(t *testing.T) {
		id := newOrder()
		original, err := repository.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		categoryID, itemID := uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO menu_categories(id,restaurant_id,name) VALUES($1,$2,'Category')`, categoryID, original.RestaurantID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO menu_items(id,restaurant_id,category_id,external_id,name,price) VALUES($1,$2,$3,'partner-original','Original',123)`, itemID, original.RestaurantID, categoryID); err != nil {
			t.Fatal(err)
		}
		// An old row predates the migration and has no reconstructible partner ID.
		legacyID := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO order_items(id,order_id,menu_item_id,name,unit_price,quantity,total_price) VALUES($1,$2,$3,'Original',123,1,123)`, legacyID, id, itemID); err != nil {
			t.Fatal(err)
		}
		// Reapplying the additive migration never guesses historical IDs.
		if _, err := pool.Exec(ctx, string(snapshotMigration)); err != nil {
			t.Fatal(err)
		}
		legacy, err := repository.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if legacy.Items[0].ExternalMenuItemID != "" {
			t.Fatal("legacy ID was guessed")
		}
		now := time.Now()
		o := &d.Order{ID: uuid.New(), UserID: "snapshot-user", RestaurantID: original.RestaurantID, Status: d.OrderStatusIntegrationFailed, DeliveryAddress: "Test address", IdempotencyKey: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
		o.Items = []d.OrderItem{{ID: uuid.New(), OrderID: o.ID, MenuItemID: itemID, ExternalMenuItemID: "partner-original", Name: "Original", UnitPrice: 123, Quantity: 1, TotalPrice: 123, CreatedAt: now}}
		if _, err := repository.CreateWithIdempotencyKey(ctx, o); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE menu_items SET external_id='partner-changed',name='Changed',price=999 WHERE id=$1`, itemID); err != nil {
			t.Fatal(err)
		}
		loaded, err := repository.GetByID(ctx, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Items) != 1 || loaded.Items[0].ExternalMenuItemID != "partner-original" || loaded.Items[0].UnitPrice != 123 {
			t.Fatalf("snapshot changed: %+v", loaded.Items)
		}
		// Duplicate creation reads items through the transactional query path.
		duplicate, err := repository.CreateWithIdempotencyKey(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		if duplicate.Items[0].ExternalMenuItemID != "partner-original" {
			t.Fatal("transactional read lost partner ID")
		}
	})
	t.Run("atomic cart mutations", func(t *testing.T) {
		carts := cartrepo.NewCartRepository(pool)
		makeItem := func() (uuid.UUID, cartdomain.CartItem) {
			id := newOrder()
			o, err := repository.GetByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			cat, item := uuid.New(), uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO menu_categories(id,restaurant_id,name) VALUES($1,$2,'Category')`, cat, o.RestaurantID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO menu_items(id,restaurant_id,category_id,external_id,name,price) VALUES($1,$2,$3,$4,'Item',100)`, item, o.RestaurantID, cat, item.String()); err != nil {
				t.Fatal(err)
			}
			return o.RestaurantID, cartdomain.CartItem{MenuItemID: item, Name: "Item", UnitPrice: 100, Quantity: 1}
		}
		rid, item := makeItem()
		user := uuid.NewString()
		results := make(chan error, 80)
		var wg sync.WaitGroup
		for i := 0; i < 80; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := carts.AddToCart(ctx, user, rid, item); results <- err }()
		}
		wg.Wait()
		close(results)
		accepted := 0
		for err := range results {
			if err == nil {
				accepted++
			} else if !errors.Is(err, coreerrors.ErrInvalidArgument) {
				t.Fatal(err)
			}
		}
		c, err := carts.GetByUserID(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if accepted != 50 || len(c.Items) != 1 || c.Items[0].Quantity != 50 || c.TotalAmount != 5000 {
			t.Fatalf("accepted=%d cart=%+v", accepted, c)
		}
		if _, err := carts.SetQuantity(ctx, "stranger", c.Items[0].ID, 2); !errors.Is(err, coreerrors.ErrNotFound) {
			t.Fatalf("ownership: %v", err)
		}
		if _, err := carts.SetQuantity(ctx, user, c.Items[0].ID, 51); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatal("limit not enforced")
		}
		// Absolute quantity updates and relative additions have a serial outcome.
		if _, err := carts.SetQuantity(ctx, user, c.Items[0].ID, 1); err != nil {
			t.Fatal(err)
		}
		wg.Add(2)
		errs := make(chan error, 2)
		go func() { defer wg.Done(); _, err := carts.SetQuantity(ctx, user, c.Items[0].ID, 10); errs <- err }()
		go func() { defer wg.Done(); _, err := carts.AddToCart(ctx, user, rid, item); errs <- err }()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		c, err = carts.GetByUserID(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if c.Items[0].Quantity != 10 && c.Items[0].Quantity != 11 {
			t.Fatalf("quantity=%d", c.Items[0].Quantity)
		}
		if c.TotalAmount != 100*int64(c.Items[0].Quantity) {
			t.Fatal("inconsistent total")
		}

		// Remove and add also have a serial outcome: empty, or one new item.
		idToRemove := c.Items[0].ID
		wg.Add(2)
		errs = make(chan error, 2)
		go func() { defer wg.Done(); _, err := carts.RemoveCartItem(ctx, user, idToRemove); errs <- err }()
		go func() { defer wg.Done(); _, err := carts.AddToCart(ctx, user, rid, item); errs <- err }()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		c, err = carts.GetByUserID(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Items) > 1 || len(c.Items) == 1 && c.Items[0].Quantity != 1 {
			t.Fatalf("remove/add outcome: %+v", c.Items)
		}
		if len(c.Items) == 0 {
			c, err = carts.AddToCart(ctx, user, rid, item)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := carts.RemoveCartItem(ctx, user, c.Items[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, err := carts.RemoveCartItem(ctx, user, c.Items[0].ID); !errors.Is(err, coreerrors.ErrNotFound) {
			t.Fatalf("missing removal: %v", err)
		}
		// Empty carts may switch restaurant; nonempty carts cannot mix partners.
		other, otherItem := makeItem()
		if _, err := carts.AddToCart(ctx, user, other, otherItem); err != nil {
			t.Fatal(err)
		}
		if _, err := carts.AddToCart(ctx, user, rid, item); !errors.Is(err, coreerrors.ErrConflict) {
			t.Fatalf("mixed restaurant: %v", err)
		}
		if err := carts.ClearByUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		if err := carts.ClearByUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		// Two different restaurants racing to create the first cart: one winner.
		user = uuid.NewString()
		wg.Add(2)
		errs = make(chan error, 2)
		go func() { defer wg.Done(); _, err := carts.AddToCart(ctx, user, rid, item); errs <- err }()
		go func() { defer wg.Done(); _, err := carts.AddToCart(ctx, user, other, otherItem); errs <- err }()
		wg.Wait()
		close(errs)
		wins := 0
		for err := range errs {
			if err == nil {
				wins++
			} else if !errors.Is(err, coreerrors.ErrConflict) {
				t.Fatal(err)
			}
		}
		if wins != 1 {
			t.Fatalf("restaurant winners=%d", wins)
		}
	})
	t.Run("one cart creates only one order with different keys", func(t *testing.T) {
		seed := newOrder()
		parent, err := repository.GetByID(ctx, seed)
		if err != nil {
			t.Fatal(err)
		}
		user := uuid.NewString()
		cartID := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO carts(id,user_id,restaurant_id) VALUES($1,$2,$3)`, cartID, user, parent.RestaurantID); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 10)
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				now := time.Now()
				o := &d.Order{ID: uuid.New(), UserID: user, RestaurantID: parent.RestaurantID, CheckoutCartID: &cartID, Status: d.OrderStatusPending, DeliveryAddress: "Test address", IdempotencyKey: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
				_, err := repository.CreateWithIdempotencyKey(ctx, o)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		winners := 0
		for err := range results {
			if err == nil {
				winners++
			} else if !errors.Is(err, coreerrors.ErrConflict) {
				t.Fatal(err)
			}
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE user_id=$1`, user).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if winners != 1 || count != 1 {
			t.Fatalf("winners=%d orders=%d", winners, count)
		}
	})
	t.Run("item mutations invalidate checkout snapshot", func(t *testing.T) {
		seed := newOrder()
		parent, err := repository.GetByID(ctx, seed)
		if err != nil {
			t.Fatal(err)
		}
		user := uuid.NewString()
		cartID, categoryID, itemID, lineID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO carts(id,user_id,restaurant_id) VALUES($1,$2,$3)`, cartID, user, parent.RestaurantID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO menu_categories(id,restaurant_id,name) VALUES($1,$2,'Category')`, categoryID, parent.RestaurantID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO menu_items(id,restaurant_id,category_id,external_id,name,price) VALUES($1,$2,$3,'partner','Item',100)`, itemID, parent.RestaurantID, categoryID); err != nil {
			t.Fatal(err)
		}
		var previous int64
		for _, sql := range []string{
			`INSERT INTO cart_items(id,cart_id,menu_item_id,name,unit_price,quantity,total_price) VALUES($1,$2,$3,'Item',100,1,100)`,
			`UPDATE cart_items SET quantity=2,total_price=200 WHERE id=$1 AND cart_id=$2 AND menu_item_id=$3`,
			`DELETE FROM cart_items WHERE id=$1 AND cart_id=$2 AND menu_item_id=$3`,
		} {
			if _, err := pool.Exec(ctx, sql, lineID, cartID, itemID); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			o := &d.Order{ID: uuid.New(), UserID: user, RestaurantID: parent.RestaurantID, CheckoutCartID: &cartID, CheckoutCartRevision: previous, Status: d.OrderStatusPending, DeliveryAddress: "Test address", IdempotencyKey: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
			if _, err := repository.CreateWithIdempotencyKey(ctx, o); !errors.Is(err, coreerrors.ErrConflict) {
				t.Fatalf("stale checkout: %v", err)
			}
			var revision int64
			if err := pool.QueryRow(ctx, `SELECT revision FROM carts WHERE id=$1`, cartID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if revision != previous+1 {
				t.Fatalf("revision %d after %d", revision, previous)
			}
			previous = revision
		}

		if _, err := pool.Exec(ctx, `INSERT INTO cart_items(id,cart_id,menu_item_id,name,unit_price,quantity,total_price) VALUES($1,$2,$3,'Item',100,1,100)`, lineID, cartID, itemID); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		current := &d.Order{ID: uuid.New(), UserID: user, RestaurantID: parent.RestaurantID, CheckoutCartID: &cartID, CheckoutCartRevision: previous + 1, Status: d.OrderStatusPending, DeliveryAddress: "Test address", IdempotencyKey: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
		if _, err := repository.CreateWithIdempotencyKey(ctx, current); err != nil {
			t.Fatal(err)
		}
		var remaining int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM cart_items WHERE cart_id=$1`, cartID).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 {
			t.Fatal("consumed cart items remain")
		}
	})
	t.Run("checkout consumes cart atomically and retries preserve new cart", func(t *testing.T) {
		seed := newOrder()
		parent, err := repository.GetByID(ctx, seed)
		if err != nil {
			t.Fatal(err)
		}
		user := uuid.NewString()
		cartID := uuid.New()
		now := time.Now()
		if _, err := pool.Exec(ctx, `INSERT INTO carts(id,user_id,restaurant_id) VALUES($1,$2,$3)`, cartID, user, parent.RestaurantID); err != nil {
			t.Fatal(err)
		}
		o := &d.Order{ID: uuid.New(), UserID: user, RestaurantID: parent.RestaurantID, CheckoutCartID: &cartID, Status: d.OrderStatusPending, DeliveryAddress: "Test address", IdempotencyKey: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
		external := o.ID.String()
		o.ExternalOrderID = &external
		if _, err := repository.CreateWithIdempotencyKey(ctx, o); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM carts WHERE id=$1`, cartID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("original cart remains")
		}
		newCart := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO carts(id,user_id,restaurant_id) VALUES($1,$2,$3)`, newCart, user, parent.RestaurantID); err != nil {
			t.Fatal(err)
		}
		// A duplicate checkout must not consume a subsequent cart.
		o.CheckoutCartID = &newCart
		if _, err := repository.CreateWithIdempotencyKey(ctx, o); err != nil {
			t.Fatal(err)
		}
		if err := repository.SaveIntegrationResult(ctx, o.ID, external, d.OrderStatusIntegrationFailed); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- repository.PrepareIntegrationRetry(ctx, o.ID, external) }()
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, coreerrors.ErrConflict) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("retry winners=%d", successes)
		}
		if err := repository.SaveIntegrationResult(ctx, o.ID, external, d.OrderStatusConfirmed); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM carts WHERE id=$1`, newCart).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("new cart lost during retry")
		}
		// If the original cart cannot be consumed, no partial order is committed.
		missing := uuid.New()
		o.ID = uuid.New()
		o.IdempotencyKey = uuid.NewString()
		o.CheckoutCartID = &missing
		replacementExternal := o.ID.String()
		o.ExternalOrderID = &replacementExternal
		if _, err := repository.CreateWithIdempotencyKey(ctx, o); !errors.Is(err, coreerrors.ErrConflict) {
			t.Fatalf("missing cart error: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE id=$1`, o.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("partial order committed")
		}
	})
	t.Run("HTTP results never remove current cart", func(t *testing.T) {
		for _, status := range []d.OrderStatus{d.OrderStatusConfirmed, d.OrderStatusCancelled, d.OrderStatusIntegrationFailed} {
			id := newOrder()
			order, err := repository.GetByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			cartID := uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO carts(id,user_id,restaurant_id) VALUES($1,$2,$3)`, cartID, cartID.String(), order.RestaurantID); err != nil {
				t.Fatal(err)
			}
			if err := repository.SaveIntegrationResult(ctx, id, id.String(), status); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM carts WHERE id=$1`, cartID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if count != want {
				t.Fatalf("status=%s cart count=%d want=%d", status, count, want)
			}
		}
	})

	t.Run("HTTP acceptance and rejection", func(t *testing.T) {
		for _, status := range []d.OrderStatus{d.OrderStatusConfirmed, d.OrderStatusCancelled} {
			id := newOrder()
			if err := repository.SaveIntegrationResult(ctx, id, id.String(), status); err != nil {
				t.Fatal(err)
			}
			assertState(id, status, 1)
			if err := repository.SaveIntegrationResult(ctx, id, id.String(), status); err != nil {
				t.Fatal(err)
			}
			assertState(id, status, 1)
		}
	})
	t.Run("early callback beats stale HTTP result", func(t *testing.T) {
		for _, result := range []d.OrderStatus{d.OrderStatusConfirmed, d.OrderStatusIntegrationFailed} {
			id := newOrder()
			order, err := repository.GetByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			found, err := repository.GetByExternalOrderIDAndRestaurantID(ctx, id.String(), order.RestaurantID)
			if err != nil || found == nil {
				t.Fatalf("early lookup: %v", err)
			}
			if err := apply(id, d.OrderStatusConfirmed, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			if err := apply(id, d.OrderStatusPreparing, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			if err := repository.SaveIntegrationResult(ctx, id, id.String(), result); err != nil {
				t.Fatal(err)
			}
			assertState(id, d.OrderStatusPreparing, 2)
		}
	})
	t.Run("rejected callback beats response", func(t *testing.T) {
		id := newOrder()
		if err := apply(id, d.OrderStatusCancelled, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		if err := repository.SaveIntegrationResult(ctx, id, id.String(), d.OrderStatusConfirmed); err != nil {
			t.Fatal(err)
		}
		assertState(id, d.OrderStatusCancelled, 1)
	})
	t.Run("retry exposes pending correlation before callback", func(t *testing.T) {
		id := newOrder()
		if err := repository.SaveIntegrationResult(ctx, id, id.String(), d.OrderStatusIntegrationFailed); err != nil {
			t.Fatal(err)
		}
		if err := repository.PrepareIntegrationRetry(ctx, id, id.String()); err != nil {
			t.Fatal(err)
		}
		if err := apply(id, d.OrderStatusConfirmed, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		if err := repository.SaveIntegrationResult(ctx, id, id.String(), d.OrderStatusConfirmed); err != nil {
			t.Fatal(err)
		}
		assertState(id, d.OrderStatusConfirmed, 3)
	})
	t.Run("concurrent response and callback", func(t *testing.T) {
		id := newOrder()
		errs := make(chan error, 2)
		go func() { errs <- apply(id, d.OrderStatusConfirmed, uuid.NewString()) }()
		go func() { errs <- repository.SaveIntegrationResult(ctx, id, id.String(), d.OrderStatusConfirmed) }()
		for i := 0; i < 2; i++ {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		order, err := repository.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if order.Status != d.OrderStatusConfirmed {
			t.Fatalf("status: %s", order.Status)
		}
		events := 0
		for _, h := range order.StatusHistory {
			if h.EventID != nil {
				events++
			}
		}
		if events != 1 {
			t.Fatalf("callback history entries: %d", events)
		}
	})

	t.Run("replay after later transition", func(t *testing.T) {
		id := newOrder()
		event := uuid.NewString()
		if err := apply(id, d.OrderStatusConfirmed, event); err != nil {
			t.Fatal(err)
		}
		if err := apply(id, d.OrderStatusPreparing, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		before, err := repository.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := apply(id, d.OrderStatusConfirmed, event); err != nil {
			t.Fatal(err)
		}
		after, err := repository.GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		assertState(id, d.OrderStatusPreparing, 2)
		if !before.UpdatedAt.Equal(after.UpdatedAt) {
			t.Fatal("replay changed updated_at")
		}
		if err := apply(id, d.OrderStatusDelivered, event); !errors.Is(err, coreerrors.ErrConflict) {
			t.Fatalf("changed payload: %v", err)
		}
		if err := apply(id, d.OrderStatusDelivered, uuid.NewString()); err == nil {
			t.Fatal("invalid transition accepted")
		}
		assertState(id, d.OrderStatusPreparing, 2)
	})
	t.Run("concurrent identical events", func(t *testing.T) {
		id := newOrder()
		event := uuid.NewString()
		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- apply(id, d.OrderStatusConfirmed, event) }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		assertState(id, d.OrderStatusConfirmed, 1)
		if err := apply(id, d.OrderStatusConfirmed, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		assertState(id, d.OrderStatusConfirmed, 2)
	})
	t.Run("concurrent event reuse across orders", func(t *testing.T) {
		ids := []uuid.UUID{newOrder(), newOrder()}
		event := uuid.NewString()
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func(id uuid.UUID) { defer wg.Done(); errs <- apply(id, d.OrderStatusConfirmed, event) }(id)
		}
		wg.Wait()
		close(errs)
		successes, conflicts := 0, 0
		for err := range errs {
			if err == nil {
				successes++
			} else if errors.Is(err, coreerrors.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
		}
		for _, id := range ids {
			order, err := repository.GetByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if order.Status == d.OrderStatusPending {
				assertState(id, d.OrderStatusPending, 0)
			} else {
				assertState(id, d.OrderStatusConfirmed, 1)
			}
		}
	})
}
