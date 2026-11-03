package core_pgx_pool

import (
	"context"
	"errors"
	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	"os"
	"testing"
	"time"
)

func TestOperationTimeoutsPostgres(t *testing.T) {
	raw := os.Getenv("KITCHENFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set disposable PostgreSQL URL")
	}
	pool, err := NewConnectionPool(Config{URL: raw, Timeout: 100 * time.Millisecond}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	check := func(t *testing.T, operation func(context.Context) error) {
		t.Helper()
		start := time.Now()
		err := operation(context.Background())
		if err == nil {
			t.Fatal("slow operation succeeded")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline cause lost: %v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("DB_TIMEOUT ignored")
		}
	}
	t.Run("pool exec", func(t *testing.T) {
		check(t, func(ctx context.Context) error { _, err := pool.Exec(ctx, `SELECT pg_sleep(2)`); return err })
	})
	t.Run("pool row", func(t *testing.T) {
		check(t, func(ctx context.Context) error { var v any; return pool.QueryRow(ctx, `SELECT pg_sleep(2)`).Scan(&v) })
	})
	rowsError := func(ctx context.Context, query func(context.Context, string, ...any) (db.Rows, error)) error {
		rows, err := query(ctx, `SELECT pg_sleep(2)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Err()
	}
	t.Run("pool rows", func(t *testing.T) { check(t, func(ctx context.Context) error { return rowsError(ctx, pool.Query) }) })
	for _, mode := range []string{"exec", "row", "rows"} {
		t.Run("transaction "+mode, func(t *testing.T) {
			tx, err := pool.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			check(t, func(ctx context.Context) error {
				switch mode {
				case "exec":
					_, err := tx.Exec(ctx, `SELECT pg_sleep(2)`)
					return err
				case "row":
					var v any
					return tx.QueryRow(ctx, `SELECT pg_sleep(2)`).Scan(&v)
				default:
					return rowsError(ctx, tx.Query)
				}
			})
		})
	}
	rows, err := pool.Query(context.Background(), `SELECT 1`)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var one int
	if err := pool.QueryRow(context.Background(), `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("pool unusable after timeout/close: %v", err)
	}
}
func TestRejectNonpositiveDatabaseTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		if _, err := NewConnectionPool(Config{URL: "postgres://localhost/test", Timeout: timeout}, context.Background()); err == nil {
			t.Fatal("invalid timeout accepted")
		}
	}
}
