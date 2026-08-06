package dashboard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// purgeAdvisoryLockKey is a distinct hashtext() key from the provisioning
// lock ("zeep-orbit-provision") — must never collide with it.
const purgeAdvisoryLockKey = "zeep-orbit-purge"

// PurgeExpiredSoftDeletes hard-deletes soft-deleted rows older than
// retentionDays across every app table, and writes one audit_log entry per
// run — including zero-row runs and runs that fail partway through — so
// operators can see the job is alive and know when it didn't finish cleanly.
//
// Guarded by a non-blocking Postgres advisory lock so that, with multiple
// replicas on the same 6h ticker, only one of them actually runs a given
// purge window; the others no-op immediately instead of racing the same
// DELETEs.
func PurgeExpiredSoftDeletes(ctx context.Context, pool *db.Pool, reg *registry.Registry, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}

	var gotLock bool
	if err := pool.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, purgeAdvisoryLockKey).Scan(&gotLock); err != nil {
		return 0, fmt.Errorf("purge: acquire advisory lock: %w", err)
	}
	if !gotLock {
		// Another replica is already running this window's purge.
		return 0, nil
	}
	defer pool.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, purgeAdvisoryLockKey)

	total := 0
	var runErr error
	for _, app := range reg.Apps() {
		for _, table := range app.Tables {
			ident := pgx.Identifier{app.SchemaName, table.Name}.Sanitize()
			sql := fmt.Sprintf(
				`DELETE FROM %s WHERE deleted_at IS NOT NULL AND deleted_at < now() - $1::interval`,
				ident,
			)
			tag, err := pool.Exec(ctx, sql, fmt.Sprintf("%d days", retentionDays))
			if err != nil {
				runErr = fmt.Errorf("purge %s.%s: %w", app.SchemaName, table.Name, err)
				break
			}
			total += int(tag.RowsAffected())
		}
		if runErr != nil {
			break
		}
	}

	meta, _ := json.Marshal(map[string]any{
		"rows_deleted":   total,
		"retention_days": retentionDays,
		"completed":      runErr == nil,
		"error":          errString(runErr),
	})
	_ = InsertAuditLog(ctx, pool, "", "system", "system.purge.run", "system_config", "", "retention purge", meta, "")

	return total, runErr
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
