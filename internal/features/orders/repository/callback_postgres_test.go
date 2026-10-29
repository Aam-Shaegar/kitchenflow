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
	repository := repo.NewOrderRepository(pool)
	newOrder := func() uuid.UUID {
		t.Helper()
		rid, id := uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO restaurants(id,external_id,name,integration_base_url) VALUES($1,$2,'Test','http://restaurant')`, rid, rid.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO orders(id,user_id,restaurant_id,status,total_amount,delivery_address,idempotency_key) VALUES($1,'user',$2,'pending',100,'Test address',$3)`, id, rid, id.String()); err != nil {
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
