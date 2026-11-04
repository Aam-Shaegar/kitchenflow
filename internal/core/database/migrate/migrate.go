package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
)

type migration struct{ name, sql string }

// Run applies the pending batch and its ledger in one transaction. Migration
// files must be transaction-compatible and must not manage transactions themselves.
func Run(ctx context.Context, pool db.Pool, dir string) error {
	if dir == "" {
		dir = "migrations"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	files := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return fmt.Errorf("migration %s is empty", entry.Name())
		}
		files = append(files, migration{entry.Name(), string(body)})
	}
	if len(files) == 0 {
		return fmt.Errorf("no SQL migrations in %s", dir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// Transaction-scoped, database-wide lock: never leak a session lock into pool.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8150811)`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(filename TEXT PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	for _, file := range files {
		var applied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename=$1)`, file.name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", file.name, err)
		}
		if applied {
			continue
		}
		if _, err := tx.Exec(ctx, file.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", file.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(filename) VALUES($1)`, file.name); err != nil {
			return fmt.Errorf("record migration %s: %w", file.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
