package core_pgx_pool

import (
	"context"
	"fmt"
	"time"

	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxConnectionPool struct {
	*pgxpool.Pool
	opTimeout time.Duration
}

func NewConnectionPool(config Config, ctx context.Context) (*PgxConnectionPool, error) {
	pgxconfig, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pgxconfig: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxconfig)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pgxpool ping: %w", err)
	}
	return &PgxConnectionPool{
		Pool:      pool,
		opTimeout: config.Timeout,
	}, nil
}

func (p *PgxConnectionPool) OpTimeout() time.Duration {
	return p.opTimeout
}

func (p *PgxConnectionPool) Query(ctx context.Context, sql string, args ...any) (core_database.Rows, error) {
	rows, err := p.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapErrors(err)
	}
	return pgxRows{rows}, err
}

func (p *PgxConnectionPool) QueryRow(ctx context.Context, sql string, args ...any) core_database.Row {
	row := p.Pool.QueryRow(ctx, sql, args...)
	return pgxRow{row}
}

func (p *PgxConnectionPool) Exec(ctx context.Context, sql string, arguments ...any) (core_database.CommandTag, error) {
	tag, err := p.Pool.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, mapErrors(err)
	}
	return pgxCommandTag{tag}, nil
}

func (p *PgxConnectionPool) Begin(ctx context.Context) (core_database.Tx, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &pgxTx{tx}, nil
}
