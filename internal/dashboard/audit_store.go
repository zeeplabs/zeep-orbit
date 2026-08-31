package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type AuditEntry struct {
	ID           string          `json:"id"`
	UserID       string          `json:"user_id"`
	UserEmail    string          `json:"user_email"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id,omitempty"`
	ResourceName string          `json:"resource_name,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	IPAddress    string          `json:"ip_address,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

func InsertAuditLog(ctx context.Context, pool *db.Pool, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) error {
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}
	// user_id is a UUID column: an empty string is not valid UUID syntax and
	// would fail the insert (verified against Postgres — it errors with
	// "invalid input syntax for type uuid"). Events with no authenticated
	// actor (e.g. an unauthenticated callback) pass userID == "", which must
	// become SQL NULL, not the literal empty string.
	var userIDParam any
	if userID != "" {
		userIDParam = userID
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.audit_log (user_id, user_email, action, resource_type, resource_id, resource_name, metadata, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userIDParam, userEmail, action, resourceType, resourceID, resourceName, metadata, ip,
	)
	if err != nil {
		return fmt.Errorf("dashboard: insert audit log: %w", err)
	}
	return nil
}

// auditCategories groups action strings into the coarse filter buckets shown
// in the Audit log UI (All/Create/Modify/Delete/Auth) — a fixed lookup table,
// not a fuzzy string match, so new actions must be added here explicitly.
var auditCategories = map[string][]string{
	"create": {
		"app.create", "user.create", "data.create", "app.table.create",
		"app.token.create", "frontend_app.create", "github.template.create",
		"app_member.added",
	},
	"modify": {
		"app.update", "data.update", "config.update", "config.system.update",
		"auth.provider.update", "app.table.update", "deploy_provider.config.update",
		"app_member.role_changed", "user.role_changed", "user.password.change",
		"app.user.sessions.reset", "app.user.role_update", "github.config.update", "github.template.update",
		"app.secret.regenerate", "frontend_app.sync.regenerate", "frontend_app.sync.retry",
		"frontend_app.retry", "frontend_app.deploy.retry",
	},
	"delete": {
		"app.delete", "user.delete", "data.delete", "app.table.delete",
		"frontend_app.delete", "github.config.delete", "github.template.delete",
		"app_member.removed", "app.token.revoke",
	},
	"auth": {
		"user.login", "user.logout", "app.user.activate", "app.user.deactivate",
		"app.secret.view", "frontend_app.sync.reveal", "github.install", "bootstrap.complete",
	},
}

type AuditLogFilter struct {
	Action    string
	Category  string
	UserEmail string
	Limit     int
	Offset    int
}

func ListAuditLog(ctx context.Context, pool *db.Pool, f AuditLogFilter) ([]AuditEntry, int, error) {
	where := "WHERE 1=1"
	args := []any{}
	n := 1

	if f.Action != "" {
		where += fmt.Sprintf(" AND action = $%d", n)
		args = append(args, f.Action)
		n++
	}
	if actions, ok := auditCategories[f.Category]; ok {
		where += fmt.Sprintf(" AND action = ANY($%d)", n)
		args = append(args, actions)
		n++
	}
	if f.UserEmail != "" {
		where += fmt.Sprintf(" AND user_email ILIKE $%d", n)
		args = append(args, "%"+f.UserEmail+"%")
		n++
	}

	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var total int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM zeep_system.audit_log `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("dashboard: count audit log: %w", err)
	}

	q := fmt.Sprintf(`SELECT id, COALESCE(user_id::text, ''), user_email, action, resource_type,
		COALESCE(resource_id, ''), COALESCE(resource_name, ''),
		COALESCE(metadata, '{}'), COALESCE(ip_address, ''), created_at
		FROM zeep_system.audit_log %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, n, n+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: list audit log: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var meta []byte
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserEmail, &e.Action, &e.ResourceType,
			&e.ResourceID, &e.ResourceName, &meta, &e.IPAddress, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("dashboard: scan audit entry: %w", err)
		}
		if len(meta) > 0 {
			e.Metadata = meta
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("dashboard: audit rows: %w", err)
	}

	if entries == nil {
		entries = []AuditEntry{}
	}
	return entries, total, nil
}
