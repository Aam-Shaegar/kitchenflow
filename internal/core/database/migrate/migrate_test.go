package migrate

import (
	"context"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/health"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database/pgx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInvalidMigrationFiles(t *testing.T) {
	for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if err := Run(context.Background(), nil, dir); err == nil {
			t.Fatal("invalid directory accepted")
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001.sql"), []byte("  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), nil, dir); err == nil {
		t.Fatal("empty migration accepted")
	}
}
func TestRunPostgres(t *testing.T) {
	raw := os.Getenv("KITCHENFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set KITCHENFLOW_TEST_DATABASE_URL to disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	fixture := func(t *testing.T) *db.PgxConnectionPool {
		t.Helper()
		schema := "migration_" + uuid.NewString()[:8]
		if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		pool, err := db.NewConnectionPool(db.Config{URL: u.String(), Timeout: 10 * time.Second}, ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			pool.Close()
			if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
				t.Error(err)
			}
		})
		return pool
	}
	write := func(t *testing.T, dir, name, sql string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("readiness tracks schema database and shutdown", func(t *testing.T) {
		pool := fixture(t)
		lifecycle, stop := context.WithCancel(ctx)
		defer stop()
		ready, err := health.Ready(pool, "../../../../migrations", lifecycle)
		if err != nil {
			t.Fatal(err)
		}
		assert := func(want int) {
			t.Helper()
			w := httptest.NewRecorder()
			ready(w, httptest.NewRequest("GET", "/health/ready", nil))
			if w.Code != want {
				t.Fatalf("readiness HTTP %d want %d", w.Code, want)
			}
		}
		assert(503)
		if err := Run(ctx, pool, "../../../../migrations"); err != nil {
			t.Fatal(err)
		}
		assert(200)
		if _, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE filename='007_order_dispatch.sql'`); err != nil {
			t.Fatal(err)
		}
		assert(503)
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(filename) VALUES('007_order_dispatch.sql'); ALTER TABLE order_dispatch RENAME TO hidden_dispatch`); err != nil {
			t.Fatal(err)
		}
		assert(503)
		if _, err := pool.Exec(ctx, `ALTER TABLE hidden_dispatch RENAME TO order_dispatch`); err != nil {
			t.Fatal(err)
		}
		assert(200)
		stop()
		assert(503)
		pool.Close()
		another, err := health.Ready(pool, "../../../../migrations", ctx)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		another(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 503 {
			t.Fatal("closed database reported ready")
		}
		w = httptest.NewRecorder()
		health.Live(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 200 {
			t.Fatal("liveness depends on DB")
		}
	})
	t.Run("parallel fresh install and no-op restart", func(t *testing.T) {
		pool := fixture(t)
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- Run(ctx, pool, "../../../../migrations") }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 8 {
			t.Fatalf("applied=%d", count)
		}
		if err := Run(ctx, pool, "../../../../migrations"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("upgrade and failed batch rollback", func(t *testing.T) {
		pool := fixture(t)
		dir := t.TempDir()
		write(t, dir, "001.sql", `CREATE TABLE example(id integer PRIMARY KEY); INSERT INTO example VALUES(1)`)
		if err := Run(ctx, pool, dir); err != nil {
			t.Fatal(err)
		}
		write(t, dir, "002.sql", `ALTER TABLE example ADD COLUMN label text; INSERT INTO example(id) VALUES(2)`)
		write(t, dir, "003.sql", `SELECT nonexistent_migration_function()`)
		if err := Run(ctx, pool, dir); err == nil {
			t.Fatal("SQL failure hidden")
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM example`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("partial SQL committed")
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("partial ledger committed")
		}
		write(t, dir, "003.sql", `UPDATE example SET label='ok'`)
		if err := Run(ctx, pool, dir); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM example WHERE label='ok'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatal("upgrade not recovered")
		}
	})
	t.Run("ledger failure rolls back SQL", func(t *testing.T) {
		pool := fixture(t)
		dir := t.TempDir()
		write(t, dir, "001.sql", `CREATE TABLE example(id integer)`)
		if err := Run(ctx, pool, dir); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_ledger() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'ledger unavailable'; END $$; CREATE TRIGGER reject_ledger BEFORE INSERT ON schema_migrations FOR EACH ROW EXECUTE FUNCTION reject_ledger()`); err != nil {
			t.Fatal(err)
		}
		write(t, dir, "002.sql", `INSERT INTO example VALUES(2)`)
		if err := Run(ctx, pool, dir); err == nil {
			t.Fatal("ledger failure hidden")
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM example`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("unrecorded SQL committed")
		}
		if _, err := pool.Exec(ctx, `DROP TRIGGER reject_ledger ON schema_migrations`); err != nil {
			t.Fatal(err)
		}
		if err := Run(ctx, pool, dir); err != nil {
			t.Fatal(err)
		}
	})

}
