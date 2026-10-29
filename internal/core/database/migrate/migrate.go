package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
)

// Run brings the schema up to date by applying pending *.sql files in name order.
// Applied files are recorded in schema_migrations, so restarts stay safe and
// fresh installs converge to the same schema as long-lived databases.
func Run(ctx context.Context, pool core_database.Pool, dir string) error {
	if dir == "" {
		dir = "migrations"
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Slim images may ship without migrations; that simply means nothing to apply.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)

	for _, name := range files {
		var count int
		row := pool.QueryRow(ctx, `SELECT 1 FROM schema_migrations WHERE filename = $1`, name)
		if err := row.Scan(&count); err == nil {
			continue // this file was applied on a previous boot
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			continue
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}
