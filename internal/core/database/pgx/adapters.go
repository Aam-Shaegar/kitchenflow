package core_pgx_pool

import (
	"context"
	"errors"
	"fmt"
	"time"

	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type pgxRows struct {
	pgx.Rows
	cancel context.CancelFunc
}
type pgxRow struct {
	pgx.Row
	cancel context.CancelFunc
}
type pgxCommandTag struct{ pgconn.CommandTag }

func (r pgxCommandTag) RowsAffected() int64 { return r.CommandTag.RowsAffected() }

func (r pgxRow) Scan(dest ...any) error {
	if r.cancel != nil {
		defer r.cancel()
	}
	err := r.Row.Scan(dest...)
	if err != nil {
		return mapErrors(err)
	}
	return nil
}

func (r pgxRows) Scan(dest ...any) error {
	err := r.Rows.Scan(dest...)
	if err != nil {
		return mapErrors(err)
	}
	return nil
}
func (r pgxRows) Err() error {
	err := r.Rows.Err()
	if err != nil {
		return mapErrors(err)
	}
	return nil
}

func (r pgxRows) Close() {
	r.Rows.Close()
	if r.cancel != nil {
		r.cancel()
	}
}
func (r pgxRows) Next() bool {
	next := r.Rows.Next()
	if !next {
		r.Close()
	}
	return next
}

type pgxTx struct {
	pgx.Tx
	timeout time.Duration
}

func (t *pgxTx) Query(ctx context.Context, sql string, args ...any) (core_database.Rows, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	rows, err := t.Tx.Query(ctx, sql, args...)
	if err != nil {
		cancel()
		return nil, mapErrors(err)
	}
	return pgxRows{Rows: rows, cancel: cancel}, nil
}

func (t *pgxTx) QueryRow(ctx context.Context, sql string, args ...any) core_database.Row {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	return pgxRow{Row: t.Tx.QueryRow(ctx, sql, args...), cancel: cancel}
}

func (t *pgxTx) Exec(ctx context.Context, sql string, arguments ...any) (core_database.CommandTag, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	tag, err := t.Tx.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, mapErrors(err)
	}
	return pgxCommandTag{tag}, nil
}

func (t *pgxTx) Commit(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	if err := t.Tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", mapErrors(err))
	}
	return nil
}

func (t *pgxTx) Rollback(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	if err := t.Tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}

func mapErrors(err error) error {
	const (
		pgErrForeignKey      = "23503"
		pgErrUniqueViolation = "23505"
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return core_database.ErrNoRows
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgErrForeignKey:
			return fmt.Errorf("foreign key violation: %w: %s", core_database.ErrViolatesForeignKey, pgErr.Message)
		case pgErrUniqueViolation:
			return fmt.Errorf("unique violation: %w: %s", core_database.ErrUniqueViolation, pgErr.Message)
		default:
			return fmt.Errorf("database error (code %s): %w: %s", pgErr.Code, core_database.ErrUnknown, pgErr.Message)
		}
	}
	return fmt.Errorf("database error: %w: %w", core_database.ErrUnknown, err)
}
