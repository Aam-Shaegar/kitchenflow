package core_pgx_pool

import (
	"context"
	"errors"
	"fmt"

	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type pgxRows struct{ pgx.Rows }
type pgxRow struct{ pgx.Row }
type pgxCommandTag struct{ pgconn.CommandTag }

func (r pgxCommandTag) RowsAffected() int64 { return r.CommandTag.RowsAffected() }

func (r pgxRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	if err != nil {
		return mapErrors(err)
	}
	return nil
}

type pgxTx struct{ pgx.Tx }

func (t *pgxTx) Query(ctx context.Context, sql string, args ...any) (core_database.Rows, error) {
	rows, err := t.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapErrors(err)
	}
	return pgxRows{rows}, nil
}

func (t *pgxTx) QueryRow(ctx context.Context, sql string, args ...any) core_database.Row {
	return pgxRow{t.Tx.QueryRow(ctx, sql, args...)}
}

func (t *pgxTx) Exec(ctx context.Context, sql string, arguments ...any) (core_database.CommandTag, error) {
	tag, err := t.Tx.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, mapErrors(err)
	}
	return pgxCommandTag{tag}, nil
}

func (t *pgxTx) Commit(ctx context.Context) error {
	if err := t.Tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (t *pgxTx) Rollback(ctx context.Context) error {
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
	return fmt.Errorf("database error: %w: %s", core_database.ErrUnknown, err.Error())
}
