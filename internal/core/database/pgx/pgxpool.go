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
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("DB_TIMEOUT must be positive")
	}
	connectCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	pgxconfig, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pgxconfig: %w", err)
	}

	if pgxconfig.ConnConfig.ConnectTimeout == 0 || pgxconfig.ConnConfig.ConnectTimeout > config.Timeout {
		pgxconfig.ConnConfig.ConnectTimeout = config.Timeout
	}
	pool, err := pgxpool.NewWithConfig(connectCtx, pgxconfig)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
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
	ctx, cancel := context.WithTimeout(ctx, p.opTimeout)
	rows, err := p.Pool.Query(ctx, sql, args...)
	if err != nil {
		cancel()
		return nil, mapErrors(err)
	}
	return pgxRows{Rows: rows, cancel: cancel}, err
}

func (p *PgxConnectionPool) QueryRow(ctx context.Context, sql string, args ...any) core_database.Row {
	ctx, cancel := context.WithTimeout(ctx, p.opTimeout)
	row := p.Pool.QueryRow(ctx, sql, args...)
	return pgxRow{Row: row, cancel: cancel}
}

func (p *PgxConnectionPool) Exec(ctx context.Context, sql string, arguments ...any) (core_database.CommandTag, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opTimeout)
	defer cancel()
	tag, err := p.Pool.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, mapErrors(err)
	}
	return pgxCommandTag{tag}, nil
}

func (p *PgxConnectionPool) Begin(ctx context.Context) (core_database.Tx, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opTimeout)
	defer cancel()
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &pgxTx{Tx: tx, timeout: p.opTimeout}, nil
}
