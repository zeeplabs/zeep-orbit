package dashboard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// PurgeExpiredSoftDeletes hard-deletes soft-deleted rows older than
// retentionDays across every app table, and writes one audit_log entry
// per run (including zero-row runs) so operators can see the job is alive.
func PurgeExpiredSoftDeletes(ctx context.Context, pool *db.Pool, reg *registry.Registry, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	total := 0
	for _, app := range reg.Apps() {
		for _, table := range app.Tables {
			sql := fmt.Sprintf(
				`DELETE FROM %s.%s WHERE deleted_at IS NOT NULL AND deleted_at < now() - ($1 || ' days')::interval`,
				app.SchemaName, table.Name,
			)
			tag, err := pool.Exec(ctx, sql, retentionDays)
			if err != nil {
				return total, fmt.Errorf("purge %s.%s: %w", app.SchemaName, table.Name, err)
			}
			total += int(tag.RowsAffected())
		}
	}
	meta, _ := json.Marshal(map[string]any{"rows_deleted": total, "retention_days": retentionDays})
	_ = InsertAuditLog(ctx, pool, "", "system", "system.purge.run", "system_config", "", "retention purge", meta, "")
	return total, nil
}
