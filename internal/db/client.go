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

// EnduserRole is the Postgres role every end-user request runs as (SET LOCAL
// ROLE inside WithRLSContext) — no ownership, no BYPASSRLS, cannot log in
// directly. Bootstrapped by dashboard.ProvisionZeepSystem and granted to
// every app schema/table by provisioner.Apply (end-user-row-policies T3/T4).
const EnduserRole = "zeep_app_enduser"

// RLSClaims are the JWT identity claims exposed to native Postgres RLS
// policies as session GUCs (app.jwt_role / app.jwt_sub / app.jwt_email),
// readable from policy expressions via current_setting('app.jwt_<claim>', true).
type RLSClaims struct {
	Role  string
	Sub   string
	Email string
}

// WithRLSContext runs fn against a Querier inside a transaction that has
// switched to EnduserRole and exposed claims as session GUCs, so native
// Postgres RLS policies apply exactly like they would to a direct psql
// connection authenticated as that role — not just to requests that went
// through this server. Mirrors WithTimeout's structure (Begin/SET LOCAL/
// fn(tx)/Commit, defer Rollback) and composes its statement_timeout
// behavior instead of replacing it: when timeoutMs > 0 the same SET LOCAL
// statement_timeout runs in this transaction too.
//
// SET LOCAL / set_config(..., true) are transaction-scoped, so the role
// switch and GUCs never leak to a pooled connection reused by an unrelated
// request afterward.
//
// If EnduserRole doesn't exist, or the connecting role lacks membership in
// it, SET LOCAL ROLE fails and that error propagates here — this method
// never silently no-ops into running fn as the principal/owner role.
func (p *Pool) WithRLSContext(ctx context.Context, claims RLSClaims, timeoutMs int, fn func(q Querier) error) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if timeoutMs > 0 {
		// timeoutMs is an int from trusted global config, not user input — safe to format.
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
			return err
		}
	}

	// EnduserRole is a fixed Go constant, not user input — safe to format directly.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL ROLE %s", EnduserRole)); err != nil {
		return fmt.Errorf("db: set local role %s: %w", EnduserRole, err)
	}

	gucs := [][2]string{
		{"app.jwt_role", claims.Role},
		{"app.jwt_sub", claims.Sub},
		{"app.jwt_email", claims.Email},
	}
	for _, guc := range gucs {
		if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, guc[0], guc[1]); err != nil {
			return fmt.Errorf("db: set claim guc %s: %w", guc[0], err)
		}
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
