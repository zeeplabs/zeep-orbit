package provisioner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// Key = source type, value = allowed target types.
var safeTypeConversions = map[string][]string{
	"int4":        {"int8", "numeric", "text"},
	"int8":        {"numeric", "text"},
	"numeric":     {"text"},
	"text":        {},
	"bool":        {"text"},
	"uuid":        {"text"},
	"timestamptz": {"text"},
	"jsonb":       {"text"},
}

// pgTypeToUDT converts the output of pgType() to udt_name from information_schema.
func pgTypeToUDT(ddlType string) string {
	switch ddlType {
	case "INTEGER":
		return "int4"
	case "BIGINT":
		return "int8"
	case "DECIMAL":
		return "numeric"
	case "TEXT":
		return "text"
	case "BOOLEAN":
		return "bool"
	case "UUID":
		return "uuid"
	case "TIMESTAMPTZ":
		return "timestamptz"
	case "JSONB":
		return "jsonb"
	default:
		return "text"
	}
}

// Idempotente.
func (p *Provisioner) ensureMigrationTable(ctx context.Context, schemaName string) error {
	sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q."_schema_migrations" (
		"id"          SERIAL PRIMARY KEY,
		"migration_id" TEXT NOT NULL UNIQUE,
		"description" TEXT NOT NULL,
		"sql_executed" TEXT NOT NULL,
		"applied_at"  TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, schemaName)
	_, err := p.pool.Exec(ctx, sql)
	return err
}

// isMigrationApplied checks if a migration_id has already been executed.
func (p *Provisioner) isMigrationApplied(ctx context.Context, schemaName, migrationID string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %q."_schema_migrations" WHERE migration_id = $1)`, schemaName),
		migrationID,
	).Scan(&exists)
	return exists, err
}

// recordMigration insere um registro em _schema_migrations.
func (p *Provisioner) recordMigration(ctx context.Context, schemaName, migrationID, description, sqlExecuted string) error {
	_, err := p.pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q."_schema_migrations" (migration_id, description, sql_executed) VALUES ($1, $2, $3)`, schemaName),
		migrationID, description, sqlExecuted,
	)
	return err
}

// migrationID generates a unique identifier for a migration based on inputs.
func migrationID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", h[:8])
}

// applyRename renomeia uma coluna se o config tiver rename_from e a coluna antiga existir.
func (p *Provisioner) applyRename(ctx context.Context, schemaName, tableName string, col config.ColumnConfig, existing map[string]string) (string, error) {
	if col.RenameFrom == "" {
		return "", nil
	}
	if _, exists := existing[col.Name]; exists {
		return "", nil
	}
	if _, exists := existing[col.RenameFrom]; !exists {
		return "", nil
	}

	sql := fmt.Sprintf(`ALTER TABLE %q.%q RENAME COLUMN %q TO %q`,
		schemaName, tableName, col.RenameFrom, col.Name)

	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return "", fmt.Errorf("rename column %q to %q: %w", col.RenameFrom, col.Name, err)
	}

	mid := migrationID(schemaName, tableName, "rename", col.RenameFrom, col.Name)
	if err := p.recordMigration(ctx, schemaName, mid,
		fmt.Sprintf("rename %s.%s.%s → %s", schemaName, tableName, col.RenameFrom, col.Name),
		sql,
	); err != nil {
		return "", fmt.Errorf("rename: record migration: %w", err)
	}

	return fmt.Sprintf("%s.%s.%s (renamed from %s)", schemaName, tableName, col.Name, col.RenameFrom), nil
}

// Only allows safe (widening) conversions as per safeTypeConversions.
func (p *Provisioner) applyTypeChange(ctx context.Context, schemaName, tableName string, col config.ColumnConfig, existing map[string]string) (string, error) {
	if systemColumnNames[col.Name] {
		return "", nil
	}

	currentType, exists := existing[col.Name]
	if !exists {
		return "", nil
	}

	desiredType := pgTypeToUDT(pgType(col.Type))
	if currentType == desiredType {
		return "", nil
	}

	safeTargets, ok := safeTypeConversions[currentType]
	if !ok {
		return "", fmt.Errorf("cannot change type of %q from %s to %s: source type %s has no defined conversions",
			col.Name, currentType, desiredType, currentType)
	}

	isSafe := false
	for _, t := range safeTargets {
		if t == desiredType {
			isSafe = true
			break
		}
	}
	if !isSafe {
		return "", fmt.Errorf("cannot change type of %q from %s to %s: unsafe conversion (would narrow or lose data)",
			col.Name, currentType, desiredType)
	}

	targetType := pgType(col.Type)
	sql := fmt.Sprintf(`ALTER TABLE %q.%q ALTER COLUMN %q TYPE %s USING %q::%s`,
		schemaName, tableName, col.Name, targetType, col.Name, strings.ToLower(targetType))

	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return "", fmt.Errorf("change type of %q from %s to %s: %w", col.Name, currentType, desiredType, err)
	}

	mid := migrationID(schemaName, tableName, "altertype", col.Name, desiredType)
	if err := p.recordMigration(ctx, schemaName, mid,
		fmt.Sprintf("alter type %s.%s.%s: %s → %s", schemaName, tableName, col.Name, currentType, desiredType),
		sql,
	); err != nil {
		return "", fmt.Errorf("alter type: record migration: %w", err)
	}

	return fmt.Sprintf("%s.%s.%s (%s → %s)", schemaName, tableName, col.Name, currentType, desiredType), nil
}
