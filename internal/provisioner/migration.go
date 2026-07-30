package provisioner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// Key = source type, value = allowed target types. Every pair here relies on
// PostgreSQL's implicit/assignment cast via `USING col::type` succeeding for
// all pre-existing values, not just new ones — see per-entry rationale below.
// Pairs of pgTypeToUDT not listed were evaluated and excluded (rationale
// inline at the bottom of this block) rather than omitted by oversight.
var safeTypeConversions = map[string][]string{
	// int4 -> int8/numeric: strictly widening, every int4 value fits.
	// int4 -> text: any integer has an unambiguous decimal representation.
	"int4": {"int8", "numeric", "text"},
	// int8 -> numeric: widening (numeric is unbounded precision).
	// int8 -> text: same rationale as int4 -> text.
	"int8": {"numeric", "text"},
	// numeric -> text: always representable; no int8/int4 target because
	// numeric may hold values or scale that overflow/truncate on downcast.
	"numeric": {"text"},
	// text has no safe automatic target: a text column may hold arbitrary
	// strings, so casting to any narrower type risks a runtime failure that
	// depends entirely on existing data — left to TypeChangeError's runtime
	// failure path rather than a false "safe" guarantee here.
	"text": {},
	// bool -> text: "true"/"false" is a lossless, unambiguous representation.
	"bool": {"text"},
	// uuid -> text: canonical UUID string form is lossless.
	"uuid": {"text"},
	// timestamptz -> text: ISO 8601 representation is lossless.
	"timestamptz": {"text"},
	// jsonb -> text: serialized JSON text is lossless.
	"jsonb": {"text"},
}

// Pairs evaluated and excluded from safeTypeConversions (pgTypeToUDT covers
// int4, int8, numeric, text, bool, uuid, timestamptz, jsonb):
//   - numeric/int8 -> int4, numeric -> int8: narrowing, may overflow — unsafe by definition.
//   - text -> anything: source may contain data incompatible with the target type — unsafe by definition.
//   - bool/uuid/timestamptz/jsonb -> int4/int8/numeric: no meaningful implicit cast — unsafe by definition.
//   - bool -> uuid/timestamptz/jsonb, uuid -> bool/timestamptz/jsonb, timestamptz -> bool/uuid/jsonb: unrelated domains — unsafe by definition.
//   - jsonb -> bool/uuid/timestamptz: would require the JSON value to already encode that exact scalar — not guaranteed, unsafe by definition.

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
		return "", &TypeChangeError{Column: col.Name, CurrentType: currentType, DesiredType: desiredType, Reason: ReasonNoConversionsDefined}
	}

	isSafe := false
	for _, t := range safeTargets {
		if t == desiredType {
			isSafe = true
			break
		}
	}
	if !isSafe {
		return "", &TypeChangeError{Column: col.Name, CurrentType: currentType, DesiredType: desiredType, Reason: ReasonUnsafeNarrowing}
	}

	targetType := pgType(col.Type)
	sql := fmt.Sprintf(`ALTER TABLE %q.%q ALTER COLUMN %q TYPE %s USING %q::%s`,
		schemaName, tableName, col.Name, targetType, col.Name, strings.ToLower(targetType))

	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return "", &TypeChangeError{Column: col.Name, CurrentType: currentType, DesiredType: desiredType, Reason: ReasonRuntimeFailure, Cause: err}
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
