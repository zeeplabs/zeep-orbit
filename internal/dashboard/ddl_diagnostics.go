package dashboard

// ddl_diagnostics.go — multi-replica-consistency T12. When DDL gives up
// waiting for a lock, log which sessions held locks in that app's schema
// (or the app's DDL advisory lock), so an operator can find a stuck query
// or a long transaction without restarting pods (restarting destroyed the
// evidence in the original incident).
//
// Only pid, state, backend type, timestamps, lock mode and relation are
// logged: never the query text (may carry user data), the role name or the
// client address.

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ddlLockHolder is one granted lock that may have blocked a DDL statement.
type ddlLockHolder struct {
	PID         int32      `json:"pid"`
	State       string     `json:"state"`
	BackendType string     `json:"backend_type"`
	XactStart   *time.Time `json:"xact_start,omitempty"`
	StateChange *time.Time `json:"state_change,omitempty"`
	Mode        string     `json:"mode"`
	Relation    string     `json:"relation,omitempty"`
}

// ddlLockHoldersSQL lists granted locks on relations in schema $1, plus the
// per-app advisory lock (db.LockAppDDL keys it by hashtextextended of $2; a
// 64-bit advisory key is split across classid (high) and objid (low)).
const ddlLockHoldersSQL = `
SELECT a.pid, COALESCE(a.state, ''), COALESCE(a.backend_type, ''), a.xact_start, a.state_change,
       l.mode, COALESCE(c.relname, '')
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
LEFT JOIN pg_class c ON c.oid = l.relation
LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE l.granted
  AND l.pid <> pg_backend_pid()
  AND (
    n.nspname = $1
    OR (l.locktype = 'advisory' AND l.objsubid = 1
        AND ((l.classid::bigint << 32) | l.objid::bigint) = hashtextextended($2, 0))
  )
ORDER BY a.xact_start NULLS LAST
LIMIT 20`

func (h *Handler) ddlLockHolders(ctx context.Context, schema string) ([]ddlLockHolder, error) {
	rows, err := h.pool.Query(ctx, ddlLockHoldersSQL, schema, "zeep-orbit-ddl:"+schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var holders []ddlLockHolder
	for rows.Next() {
		var hl ddlLockHolder
		if err := rows.Scan(&hl.PID, &hl.State, &hl.BackendType, &hl.XactStart, &hl.StateChange, &hl.Mode, &hl.Relation); err != nil {
			return nil, err
		}
		holders = append(holders, hl)
	}
	return holders, rows.Err()
}

// logDDLLockHolders logs the lock holders for schema at Warn level. It runs
// on its own short timeout so a slow catalog query never delays the 409.
func (h *Handler) logDDLLockHolders(ctx context.Context, schema string, cause error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	holders, err := h.ddlLockHolders(ctx, schema)
	if err != nil {
		h.logger.Warn("ddl lock timeout: could not list lock holders",
			zap.String("schema", schema), zap.NamedError("cause", cause), zap.Error(err))
		return
	}
	if len(holders) == 0 {
		h.logger.Warn("ddl lock timeout: waiting on per-app DDL lock held by another request (holder already gone)",
			zap.String("schema", schema), zap.NamedError("cause", cause))
		return
	}
	h.logger.Warn("ddl lock timeout: lock held by other sessions",
		zap.String("schema", schema), zap.NamedError("cause", cause), zap.Any("holders", holders))
}
