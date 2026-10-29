package core_database

import (
	"context"
	"errors"
	"time"
)

type Pool interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, arguments ...any) (CommandTag, error)
	Close()
	OpTimeout() time.Duration
	Begin(ctx context.Context) (Tx, error)
}

type Tx interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, arguments ...any) (CommandTag, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Rows interface {
	Close()
	Next() bool
	Scan(dest ...any) error
	Err() error
}

type Row interface {
	Scan(dest ...any) error
}

type CommandTag interface {
	RowsAffected() int64
}

var (
	ErrNoRows             = errors.New("no rows")
	ErrViolatesForeignKey = errors.New("violates foreign key")
	ErrUniqueViolation    = errors.New("unique violation")
	ErrUnknown            = errors.New("unknown")
)
