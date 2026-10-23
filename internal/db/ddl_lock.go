package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DDLLockTimeout bounds how long a DDL transaction waits for a lock — both
// the per-app advisory lock taken by LockAppDDL and any table lock (e.g. the
// ACCESS EXCLUSIVE an ALTER TABLE needs) taken later in the same
// transaction. Without it, an ALTER queued behind a long-lived reader blocks
// every later query on that table until the reader finishes, which is how a
// multi-replica deploy froze table create/edit until the pods restarted.
// A var, not a const, so tests can shorten it.
var DDLLockTimeout = 5 * time.Second

// ddlLockNamespace prefixes the advisory-lock key so it can't collide with
// advisory locks taken elsewhere for other purposes.
const ddlLockNamespace = "zeep-orbit-ddl:"

// LockAppDDL bounds lock waits for the rest of tx to DDLLockTimeout and then
// takes a transaction-scoped advisory lock keyed by the app's schema name, so
// DDL for one app is serialized across every replica while DDL for other
// apps proceeds in parallel. Call it as the first statement of a DDL
// transaction. SET LOCAL and pg_advisory_xact_lock are both released at
// commit/rollback, so nothing leaks to the pooled connection.
//
// A wait longer than DDLLockTimeout fails with SQLSTATE 55P03 (see
// IsLockTimeout), which aborts tx.
func LockAppDDL(ctx context.Context, tx pgx.Tx, schemaName string) error {
	// DDLLockTimeout is a trusted package var, not user input — safe to format.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", DDLLockTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("db: set lock_timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, ddlLockNamespace+schemaName); err != nil {
		return fmt.Errorf("db: lock app ddl %s: %w", schemaName, err)
	}
	return nil
}

// IsLockTimeout reports whether err is a Postgres lock-wait abort
// (SQLSTATE 55P03, lock_not_available) — what lock_timeout raises.
func IsLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}
