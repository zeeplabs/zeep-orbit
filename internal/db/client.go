package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool to allow future extension without breaking the external contract.
type Pool struct {
	*pgxpool.Pool
}

// limitar o tempo de espera (recomendado: 5s).
func New(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: invalid DSN: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping failed: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	p.Pool.Close()
}

// Querier is the subset of query methods shared by *pgxpool.Pool and pgx.Tx,
// so a caller can run the same query with or without a timeout transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// WithTimeout runs fn against a Querier. When timeoutMs > 0, fn runs inside a
// short transaction with SET LOCAL statement_timeout, so Postgres aborts a query
// that runs longer; SET LOCAL is transaction-scoped, so the limit never leaks to
// the pooled connection. When timeoutMs <= 0, fn runs directly on the pool with
// no transaction (timeout disabled), preserving the pre-timeout behavior.
func (p *Pool) WithTimeout(ctx context.Context, timeoutMs int, fn func(q Querier) error) error {
	if timeoutMs <= 0 {
		return fn(p.Pool)
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// timeoutMs is an int from trusted global config, not user input — safe to format.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsStatementTimeout reports whether err is a Postgres statement-timeout abort
// (SQLSTATE 57014, query_canceled).
func IsStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}
