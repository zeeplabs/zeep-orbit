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
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.audit_log (user_id, user_email, action, resource_type, resource_id, resource_name, metadata, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip,
	)
	if err != nil {
		return fmt.Errorf("dashboard: insert audit log: %w", err)
	}
	return nil
}

type AuditLogFilter struct {
	Action string
	UserID string
	Limit  int
	Offset int
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
	if f.UserID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", n)
		args = append(args, f.UserID)
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

	q := fmt.Sprintf(`SELECT id, user_id, user_email, action, resource_type,
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

func auditActionLabel(a string) string {
	labels := map[string]string{
		"app.create":              "App Criado",
		"app.update":              "App Atualizado",
		"app.delete":              "App Excluído",
		"user.create":             "Usuário Criado",
		"user.delete":             "Usuário Excluído",
		"user.login":              "Login",
		"user.logout":             "Logout",
		"user.password.change":    "Senha Alterada",
		"config.update":           "Configuração Alterada",
		"auth.provider.update":    "Provedor Auth Atualizado",
		"app.user.deactivate":     "Usuário App Desativado",
		"app.user.activate":       "Usuário App Ativado",
		"app.user.sessions.reset": "Sessões App Resetadas",
		"data.create":             "Registro Criado",
		"data.update":             "Registro Atualizado",
		"data.delete":             "Registro Excluído",
		"bootstrap.complete":      "Bootstrap Concluído",
	}
	if l, ok := labels[a]; ok {
		return l
	}
	return a
}
