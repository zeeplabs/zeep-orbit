package provisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ErrDDLLockTimeout is returned (wrapped) when a DDL operation waited longer
// than db.DDLLockTimeout for a lock — the per-app DDL lock held by another
// request, or a table lock held by a long-running query. The transaction is
// rolled back, so nothing was changed. The message is safe to show to users.
var ErrDDLLockTimeout = errors.New("table is busy with another operation, try again in a few seconds")

// DDLLockTimeoutError is a lock-timeout failure tagged with the app schema
// it happened in, so the caller can log who held the lock. errors.Is(err,
// ErrDDLLockTimeout) is true for it and Error() is ErrDDLLockTimeout's
// message; the Postgres error stays reachable through Unwrap.
type DDLLockTimeoutError struct {
	Schema string
	Err    error
}

func (e *DDLLockTimeoutError) Error() string { return ErrDDLLockTimeout.Error() }

func (e *DDLLockTimeoutError) Unwrap() []error { return []error{ErrDDLLockTimeout, e.Err} }

// WrapLockTimeout tags err as a *DDLLockTimeoutError for schemaName when it
// is a Postgres lock timeout (55P03); any other error is returned as is.
func WrapLockTimeout(schemaName string, err error) error {
	if err == nil || !db.IsLockTimeout(err) || errors.Is(err, ErrDDLLockTimeout) {
		return err
	}
	return &DDLLockTimeoutError{Schema: schemaName, Err: err}
}

// withAppLock runs fn in one transaction holding the per-app DDL lock for
// schemaName. fn gets a Provisioner bound to that transaction; calls it
// makes to other exported methods run inline instead of locking again.
// Any error rolls the whole transaction back, and a lock wait that hit
// db.DDLLockTimeout is reported as a *DDLLockTimeoutError.
func (p *Provisioner) withAppLock(ctx context.Context, schemaName string, fn func(tp *Provisioner) error) error {
	if p.locked {
		return fn(p)
	}
	tx, err := p.q.Begin(ctx)
	if err != nil {
		return fmt.Errorf("provisioner: begin ddl transaction for %q: %w", schemaName, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := db.LockAppDDL(ctx, tx, schemaName); err != nil {
		return WrapLockTimeout(schemaName, err)
	}
	if err := fn(&Provisioner{q: tx, locked: true}); err != nil {
		return WrapLockTimeout(schemaName, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return WrapLockTimeout(schemaName, fmt.Errorf("provisioner: commit ddl transaction for %q: %w", schemaName, err))
	}
	return nil
}

// EnsureAuthTables provisions the "_auth_users"/"_auth_sessions" tables for
// one app's schema, without touching any other app or table. Idempotent —
// safe to call every time an app's auth_email_enabled flips to true,
// including when it was already true. Use this instead of Apply when only
// auth needs provisioning: Apply reconciles every table in the app's config,
// which is the wrong blast radius for a request that only toggles auth.
func (p *Provisioner) EnsureAuthTables(ctx context.Context, schemaName string) error {
	return p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		return tp.ensureAuthTables(ctx, schemaName)
	})
}

// EnsureStorageTables provisions the "_files" table for one app's schema,
// without touching any other app or table. Idempotent — safe to call every
// time an app's storage bucket is set, including when it was already set.
// Use this instead of Apply for the same reason as EnsureAuthTables.
func (p *Provisioner) EnsureStorageTables(ctx context.Context, schemaName string) error {
	return p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		return tp.ensureStorageTables(ctx, schemaName)
	})
}

// Safe to call on every request. It first checks the catalog without
// locking anything and returns when every column and the email index exist,
// which is the normal case: ALTER TABLE ... ADD COLUMN IF NOT EXISTS takes an
// ACCESS EXCLUSIVE lock even when the column is already there, so running it
// unconditionally on each dashboard request queued behind any open read on
// _auth_users and blocked every login behind it.
func (p *Provisioner) EnsureAuthUserColumns(ctx context.Context, schema string) error {
	if done, err := p.authUserColumnsPresent(ctx, schema); err == nil && done {
		return nil
	}
	return p.withAppLock(ctx, schema, func(tp *Provisioner) error {
		return tp.ensureAuthUserColumns(ctx, schema)
	})
}

// ApplyColumnConstraintChanges runs the DDL for every change in one
// transaction — all applied or none (CCC-05). The pre-check runs again
// inside the transaction so a row written between CheckColumnConstraintChanges
// and this call still yields a typed error rather than a raw Postgres one.
//
// Every statement is idempotent against a database that already matches the
// requested state (CCC-06): SET/DROP NOT NULL and SET/DROP DEFAULT are no-ops
// in that case, and UNIQUE is only added when no single-column unique
// constraint already covers the column.
//
// Returns one "schema.table.column (description)" entry per applied change,
// the same format applyColumnChanges reports.
func (p *Provisioner) ApplyColumnConstraintChanges(ctx context.Context, schemaName, tableName string, changes []ColumnConstraintChange) ([]string, error) {
	var applied []string
	err := p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		var err error
		applied, err = tp.applyColumnConstraintChanges(ctx, schemaName, tableName, changes)
		return err
	})
	return applied, err
}

// DropTable drops a table if it exists. Used when a single table is removed
// from an app without touching the app's other tables. Refuses to drop a
// table still referenced by another table's foreign key — the dependent FK
// must be removed first (own apply or prior apply), rather than silently
// leaving a dangling reference or failing with a raw Postgres error.
func (p *Provisioner) DropTable(ctx context.Context, schemaName, tableName string) error {
	return p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		return tp.dropTable(ctx, schemaName, tableName)
	})
}

// AddColumnForeignKey runs ALTER TABLE ... ADD FOREIGN KEY on an existing
// column. The constraint is left unnamed so Postgres applies its own
// "<table>_<column>_fkey" convention — identical naming to a column created
// with an inline REFERENCES clause (columnDDL), so a FK's origin (added at
// creation vs. added later) is never distinguishable by name. A Postgres FK
// violation (orphaned rows, code 23503) is translated into a typed, safe-
// to-expose *ForeignKeyViolationError carrying Postgres's own Detail text.
func (p *Provisioner) AddColumnForeignKey(ctx context.Context, schemaName, tableName, columnName string, ref config.ReferenceConfig) error {
	return p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		return tp.addColumnForeignKey(ctx, schemaName, tableName, columnName, ref)
	})
}

// DropColumnForeignKey locates the real FK constraint on a column via the
// Postgres catalog (never by assuming a naming convention — see
// AddColumnForeignKey's comment on why a FK's origin is not name-derivable)
// and drops it. found=false, err=nil means no FK constraint currently
// exists on that column — the caller treats this as convergence (a stale
// stored schema catching up to reality), not an error.
func (p *Provisioner) DropColumnForeignKey(ctx context.Context, schemaName, tableName, columnName string) (bool, error) {
	var found bool
	err := p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		var err error
		found, err = tp.dropColumnForeignKey(ctx, schemaName, tableName, columnName)
		return err
	})
	return found, err
}

// ReplaceColumnEnumValues replaces the CHECK constraint restricting an enum
// column to oldValues with one restricting it to newValues.
//
// A narrowing (any value in oldValues missing from newValues) is pre-checked:
// if existing rows still hold a value being removed, the call fails with a
// typed *EnumValueInUseError naming every offending value and its exact row
// count, before any DDL runs — so a rejected narrow leaves the current
// constraint completely untouched. A pure widening skips the pre-check, since
// every existing value is still a member of the larger set.
//
// The swap is a single ALTER TABLE statement carrying both the DROP and the
// ADD, so Postgres applies both or neither: the table is never left without a
// constraint. The new constraint is deliberately left unnamed so Postgres
// applies its own "<table>_<column>_check" convention — identical to a column
// created with an inline CHECK (columnDDL), so a constraint's origin (created
// with the column vs. replaced later) is never distinguishable by name.
func (p *Provisioner) ReplaceColumnEnumValues(ctx context.Context, schemaName, tableName, columnName string, oldValues, newValues []string) error {
	return p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
		return tp.replaceColumnEnumValues(ctx, schemaName, tableName, columnName, oldValues, newValues)
	})
}
