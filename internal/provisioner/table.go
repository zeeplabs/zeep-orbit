package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// systemColumnNames lists the reserved column names managed by the server; they
// are injected automatically on every table and should not be defined by the user.
var systemColumnNames = map[string]bool{
	"id":         true,
	"created_at": true,
	"updated_at": true,
	"owner_id":   true,
	"deleted_at": true,
}

func pgType(t string) string {
	switch t {
	case "text":
		return "TEXT"
	case "integer":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "decimal", "numeric":
		return "DECIMAL"
	case "boolean":
		return "BOOLEAN"
	case "uuid":
		return "UUID"
	case "timestamptz":
		return "TIMESTAMPTZ"
	case "jsonb":
		return "JSONB"
	case "enum":
		// enum is not a native Postgres type here: the column is TEXT with a
		// CHECK (col IN (...)) constraint (see columnDDL). Spelled out rather
		// than left to the default branch so the intent is explicit.
		return "TEXT"
	default:
		return "TEXT"
	}
}

// quotedValueList renders an enum column's allowed values as a
// comma-separated list of SQL string literals for a CHECK (col IN (...))
// clause. Values are arbitrary free text (accents, spaces, quotes are all
// valid — see config.ValidateEnumValues), so each single quote is doubled,
// the same escaping columnDDL already applies to a literal DEFAULT.
func quotedValueList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
	}
	return strings.Join(quoted, ", ")
}

// onDeleteSQL translates a ReferenceConfig.OnDelete value to its SQL clause.
func onDeleteSQL(onDelete string) string {
	switch onDelete {
	case "cascade":
		return "CASCADE"
	case "restrict":
		return "RESTRICT"
	case "set_null":
		return "SET NULL"
	default:
		return "NO ACTION"
	}
}

// Single quotes in a literal DEFAULT value are escaped by doubling them
// (”). When col.DefaultIsExpression is set, col.Default is written
// unquoted instead (e.g. "now()", "gen_random_uuid()") — callers must have
// already validated it against the allowlist in config.ValidateTables
// (validateDefault), since this function trusts col.Type/col.Default the
// same way it already trusts col.Type without re-checking allowedTypes.
// schemaName is needed to schema-qualify REFERENCES targets (references are
// intra-app, so the target table lives in the same schema as this column's
// table).
func columnDDL(schemaName string, col config.ColumnConfig) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%q %s", col.Name, pgType(col.Type)))

	if col.Required {
		sb.WriteString(" NOT NULL")
	}
	if col.Default != "" {
		if col.DefaultIsExpression {
			sb.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
		} else {
			escaped := strings.ReplaceAll(col.Default, "'", "''")
			sb.WriteString(fmt.Sprintf(" DEFAULT '%s'", escaped))
		}
	}
	if col.Unique {
		sb.WriteString(" UNIQUE")
	}
	if col.Type == "enum" {
		sb.WriteString(fmt.Sprintf(" CHECK (%q IN (%s))", col.Name, quotedValueList(col.AllowedValues)))
	}
	if col.References != nil {
		sb.WriteString(fmt.Sprintf(" REFERENCES %q.%q(%q) ON DELETE %s", schemaName, col.References.Table, col.References.Column, onDeleteSQL(col.References.OnDelete)))
	}

	return sb.String()
}

// Returns true if the table was created (did not exist), false if it already existed.
func (p *Provisioner) createTable(ctx context.Context, schemaName, tableName string, cols []config.ColumnConfig, rls string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`,
		schemaName, tableName,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("table: check existence %q.%q: %w", schemaName, tableName, err)
	}

	if exists {
		return false, nil
	}

	var colDefs []string
	colDefs = append(colDefs, `"id" UUID PRIMARY KEY DEFAULT gen_random_uuid()`)

	for _, col := range cols {
		if systemColumnNames[col.Name] {
			continue
		}
		colDefs = append(colDefs, columnDDL(schemaName, col))
	}

	if config.HasOwnerColumn(rls) {
		// "policy" leaves owner_id nullable: unlike "owner"/"enabled", the app
		// layer never auto-populates or filters by it here (AutoScopesByOwner
		// is false for "policy") — it's only there for a table's own native
		// policies to reference if they choose to. A row with no end-user
		// identity behind it (e.g. an inbound webhook delivery, which writes
		// under the dedicated "webhook" RLS role) has no value to put there.
		nullability := "NOT NULL"
		if rls == "policy" {
			nullability = "NULL"
		}
		colDefs = append(colDefs, fmt.Sprintf(`"owner_id" UUID %s REFERENCES %q."_auth_users"("id")`, nullability, schemaName))
	}

	colDefs = append(colDefs,
		`"created_at" TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`"updated_at" TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`"deleted_at" TIMESTAMPTZ`,
	)

	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %q.%q (%s)`,
		schemaName, tableName,
		strings.Join(colDefs, ", "),
	)

	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return false, fmt.Errorf("table: create %q.%q: %w", schemaName, tableName, err)
	}

	// "policy" tables get RLS enabled at creation, before any policy exists,
	// so the fail-closed guarantee (RLS enabled + zero policies denies
	// every row to zeep_app_enduser natively) holds from the first instant
	// the table exists — "owner"/"enabled" tables still enable RLS lazily,
	// on their first CREATE POLICY (table_policies_store.go), since they
	// already get their filtering from the application layer in the
	// meantime.
	if rls == "policy" {
		if err := EnsureRowLevelSecurity(ctx, p.pool, schemaName, tableName); err != nil {
			return false, err
		}
	}

	return true, nil
}

// EnsureRowLevelSecurity runs `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` on
// schemaName.tableName against db (a *db.Pool or a pgx.Tx — anything
// satisfying db.Querier), so callers can run it either directly on the pool
// or inside an existing transaction (e.g. dashboard.CreateTablePolicy's tx).
// It is idempotent — Postgres does not error when RLS is already enabled —
// so callers may invoke it unconditionally whenever a table needs the
// fail-closed guarantee (RLS enabled + zero policies denies every row to
// zeep_app_enduser natively, with no application-level check).
func EnsureRowLevelSecurity(ctx context.Context, pool db.Querier, schemaName, tableName string) error {
	sql := fmt.Sprintf(`ALTER TABLE %q.%q ENABLE ROW LEVEL SECURITY`, schemaName, tableName)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("table: enable row level security on %q.%q: %w", schemaName, tableName, err)
	}
	return nil
}

// RelaxOwnerColumn drops the NOT NULL constraint on owner_id, if the column
// exists and still has one. Called when a table switches into "policy" mode
// (UpdateAppTable): a table created under "owner"/"enabled" still carries
// owner_id NOT NULL, but "policy" writes (e.g. an inbound webhook, which has
// no end-user identity to populate it with) never guarantee a value. Safe to
// call unconditionally — a no-op if owner_id is already nullable or the
// table predates HasOwnerColumn (no owner_id at all).
func RelaxOwnerColumn(ctx context.Context, pool db.Querier, schemaName, tableName string) error {
	sql := fmt.Sprintf(
		`ALTER TABLE %q.%q ALTER COLUMN "owner_id" DROP NOT NULL`,
		schemaName, tableName,
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42703" { // undefined_column
			return nil
		}
		return fmt.Errorf("table: relax owner_id on %q.%q: %w", schemaName, tableName, err)
	}
	return nil
}

// checkDependents returns a description of any foreign key in the same
// schema that still references tableName, so DropTable can refuse to run
// instead of silently cascading or failing with a raw Postgres FK-violation
// error.
func (p *Provisioner) checkDependents(ctx context.Context, schemaName, tableName string) (string, error) {
	var dependentTable, dependentColumn string
	err := p.pool.QueryRow(ctx,
		`SELECT tc.table_name, kcu.column_name
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		 JOIN information_schema.constraint_column_usage ccu
		   ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY'
		   AND tc.table_schema = $1
		   AND ccu.table_name = $2
		   AND tc.table_name != $2
		 LIMIT 1`,
		schemaName, tableName,
	).Scan(&dependentTable, &dependentColumn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("table: check dependents of %q.%q: %w", schemaName, tableName, err)
	}
	return fmt.Sprintf("%s.%s", dependentTable, dependentColumn), nil
}

// DropTable drops a table if it exists. Used when a single table is removed
// from an app without touching the app's other tables. Refuses to drop a
// table still referenced by another table's foreign key — the dependent FK
// must be removed first (own apply or prior apply), rather than silently
// leaving a dangling reference or failing with a raw Postgres error.
func (p *Provisioner) DropTable(ctx context.Context, schemaName, tableName string) error {
	dependent, err := p.checkDependents(ctx, schemaName, tableName)
	if err != nil {
		return err
	}
	if dependent != "" {
		return fmt.Errorf("table: cannot drop %q.%q: still referenced by %s", schemaName, tableName, dependent)
	}

	sql := fmt.Sprintf(`DROP TABLE IF EXISTS %q.%q`, schemaName, tableName)
	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("table: drop %q.%q: %w", schemaName, tableName, err)
	}
	return nil
}

// Retorna a lista de colunas adicionadas no formato "schema.table.column".
func (p *Provisioner) addMissingColumns(ctx context.Context, schemaName, tableName string, cols []config.ColumnConfig, rls string) ([]string, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2`,
		schemaName, tableName,
	)
	if err != nil {
		return nil, fmt.Errorf("table: list columns %q.%q: %w", schemaName, tableName, err)
	}
	defer rows.Close()

	existing := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("table: scan column name: %w", err)
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("table: iterate columns: %w", err)
	}

	var added []string
	for _, col := range cols {
		if systemColumnNames[col.Name] {
			continue
		}
		if _, found := existing[col.Name]; found {
			continue
		}

		sql := fmt.Sprintf(
			`ALTER TABLE %q.%q ADD COLUMN IF NOT EXISTS %s`,
			schemaName, tableName,
			columnDDL(schemaName, col),
		)
		if _, err := p.pool.Exec(ctx, sql); err != nil {
			return nil, fmt.Errorf("table: add column %q to %q.%q: %w", col.Name, schemaName, tableName, err)
		}

		added = append(added, fmt.Sprintf("%s.%s.%s", schemaName, tableName, col.Name))
	}

	if config.HasOwnerColumn(rls) {
		if _, found := existing["owner_id"]; !found {
			sql := fmt.Sprintf(
				`ALTER TABLE %q.%q ADD COLUMN IF NOT EXISTS "owner_id" UUID REFERENCES %q."_auth_users"("id")`,
				schemaName, tableName, schemaName,
			)
			if _, err := p.pool.Exec(ctx, sql); err != nil {
				return nil, fmt.Errorf("table: add owner_id to %q.%q: %w", schemaName, tableName, err)
			}
			added = append(added, fmt.Sprintf("%s.%s.owner_id", schemaName, tableName))
		}
	}

	return added, nil
}

func (p *Provisioner) fetchExistingColumns(ctx context.Context, schemaName, tableName string) (map[string]string, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT column_name, udt_name FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2`,
		schemaName, tableName,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch columns %q.%q: %w", schemaName, tableName, err)
	}
	defer rows.Close()

	cols := make(map[string]string)
	for rows.Next() {
		var name, udt string
		if err := rows.Scan(&name, &udt); err != nil {
			return nil, fmt.Errorf("fetch columns scan: %w", err)
		}
		cols[name] = udt
	}
	return cols, rows.Err()
}

// CheckForeignKeyColumnTypesMatch compares the real physical Postgres type
// (udt_name) of an existing column against the real physical type of a
// target table/column, before any ADD FOREIGN KEY DDL runs. It uses the
// actual catalog type rather than the possibly-stale declared config type,
// since the source column already exists physically. refTableName may be
// "_auth_users" — that's a real physical table in the same schema, so
// fetchExistingColumns handles it with no special-casing.
func (p *Provisioner) CheckForeignKeyColumnTypesMatch(ctx context.Context, schemaName, tableName, columnName, refTableName, refColumnName string) error {
	sourceCols, err := p.fetchExistingColumns(ctx, schemaName, tableName)
	if err != nil {
		return err
	}
	sourceType, ok := sourceCols[columnName]
	if !ok {
		return fmt.Errorf("column %q not found on table %q.%q", columnName, schemaName, tableName)
	}

	targetCols, err := p.fetchExistingColumns(ctx, schemaName, refTableName)
	if err != nil {
		return err
	}
	targetType, ok := targetCols[refColumnName]
	if !ok {
		return fmt.Errorf("column %q not found on table %q.%q", refColumnName, schemaName, refTableName)
	}

	if sourceType != targetType {
		return fmt.Errorf("column %q has type %s but referenced column %q.%q has type %s", columnName, sourceType, refTableName, refColumnName, targetType)
	}
	return nil
}

// AddColumnForeignKey runs ALTER TABLE ... ADD FOREIGN KEY on an existing
// column. The constraint is left unnamed so Postgres applies its own
// "<table>_<column>_fkey" convention — identical naming to a column created
// with an inline REFERENCES clause (columnDDL), so a FK's origin (added at
// creation vs. added later) is never distinguishable by name. A Postgres FK
// violation (orphaned rows, code 23503) is translated into a typed, safe-
// to-expose *ForeignKeyViolationError carrying Postgres's own Detail text.
func (p *Provisioner) AddColumnForeignKey(ctx context.Context, schemaName, tableName, columnName string, ref config.ReferenceConfig) error {
	sql := fmt.Sprintf(
		`ALTER TABLE %q.%q ADD FOREIGN KEY (%q) REFERENCES %q.%q(%q) ON DELETE %s`,
		schemaName, tableName, columnName, schemaName, ref.Table, ref.Column, onDeleteSQL(ref.OnDelete),
	)
	if _, err := p.pool.Exec(ctx, sql); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
			return &ForeignKeyViolationError{Column: columnName, Detail: pgErr.Detail, Cause: err}
		}
		return fmt.Errorf("table: add foreign key on %q.%q.%q: %w", schemaName, tableName, columnName, err)
	}
	return nil
}

// DropColumnForeignKey locates the real FK constraint on a column via the
// Postgres catalog (never by assuming a naming convention — see
// AddColumnForeignKey's comment on why a FK's origin is not name-derivable)
// and drops it. found=false, err=nil means no FK constraint currently
// exists on that column — the caller treats this as convergence (a stale
// stored schema catching up to reality), not an error.
func (p *Provisioner) DropColumnForeignKey(ctx context.Context, schemaName, tableName, columnName string) (found bool, err error) {
	var constraintName string
	err = p.pool.QueryRow(ctx,
		`SELECT tc.constraint_name
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY'
		   AND tc.table_schema = $1
		   AND tc.table_name = $2
		   AND kcu.column_name = $3
		 LIMIT 1`,
		schemaName, tableName, columnName,
	).Scan(&constraintName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("table: find foreign key on %q.%q.%q: %w", schemaName, tableName, columnName, err)
	}

	sql := fmt.Sprintf(`ALTER TABLE %q.%q DROP CONSTRAINT %q`, schemaName, tableName, constraintName)
	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return false, fmt.Errorf("table: drop foreign key %q on %q.%q: %w", constraintName, schemaName, tableName, err)
	}
	return true, nil
}

// findColumnCheckConstraint locates the real name of the single-column CHECK
// constraint on a column via the Postgres catalog, never by assuming a
// naming convention (a column name may reach 63 bytes, and Postgres
// identifiers are capped at 63 too, so any "<column>_enum_check" style guess
// can silently truncate or collide) — the same reasoning
// AddColumnForeignKey/DropColumnForeignKey already document for FKs.
// found=false, err=nil means the column currently has no CHECK constraint.
//
// SPEC_DEVIATION: design.md specifies information_schema.table_constraints
// joined to key_column_usage (DropColumnForeignKey's query shape).
// Reason: key_column_usage only holds key columns (PK/UNIQUE/FK), so that
// join returns zero rows for a CHECK constraint — verified against Postgres
// 16. table_constraints alone is also unusable: it additionally lists the
// implicit "<oid>_<oid>_<n>_not_null" constraint of any NOT NULL column.
// pg_constraint filtered to contype='c' and conkey = the column's attnum
// returns exactly the real CHECK constraint. Same intent as the design
// (catalog lookup, no assumed name), correct catalog.
func (p *Provisioner) findColumnCheckConstraint(ctx context.Context, schemaName, tableName, columnName string) (name string, found bool, err error) {
	// No LIMIT 1 / ORDER BY: a column can only be widened/narrowed once its
	// own enum CHECK is uniquely identified. If it ever has more than one
	// single-column CHECK (e.g. a hand-added constraint alongside the enum
	// one), picking an arbitrary one to DROP would silently remove a
	// constraint this call was never asked to touch — fail closed instead
	// of guessing.
	rows, err := p.pool.Query(ctx,
		`SELECT c.conname
		 FROM pg_constraint c
		 JOIN pg_class r ON r.oid = c.conrelid
		 JOIN pg_namespace n ON n.oid = r.relnamespace
		 JOIN pg_attribute a ON a.attrelid = r.oid AND a.attname = $3
		 WHERE c.contype = 'c'
		   AND n.nspname = $1
		   AND r.relname = $2
		   AND c.conkey = ARRAY[a.attnum]`,
		schemaName, tableName, columnName,
	)
	if err != nil {
		return "", false, fmt.Errorf("table: find check constraint on %q.%q.%q: %w", schemaName, tableName, columnName, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", false, fmt.Errorf("table: find check constraint on %q.%q.%q: %w", schemaName, tableName, columnName, err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return "", false, fmt.Errorf("table: find check constraint on %q.%q.%q: %w", schemaName, tableName, columnName, err)
	}

	switch len(names) {
	case 0:
		return "", false, nil
	case 1:
		return names[0], true, nil
	default:
		return "", false, fmt.Errorf("table: %q.%q.%q has %d single-column CHECK constraints (%v) — cannot determine which one backs the enum values", schemaName, tableName, columnName, len(names), names)
	}
}

// removedValues returns the values present in old but not in new, in old's
// declaration order.
func removedValues(oldValues, newValues []string) []string {
	kept := make(map[string]struct{}, len(newValues))
	for _, v := range newValues {
		kept[v] = struct{}{}
	}

	var removed []string
	for _, v := range oldValues {
		if _, ok := kept[v]; !ok {
			removed = append(removed, v)
		}
	}
	return removed
}

// countRowsUsingValues counts existing rows per value, scoped to the given
// values only (never a full-table comparison in application code). Values
// with no rows are absent from the result.
func (p *Provisioner) countRowsUsingValues(ctx context.Context, schemaName, tableName, columnName string, values []string) (map[string]int, error) {
	sql := fmt.Sprintf(
		`SELECT %q, COUNT(*) FROM %q.%q WHERE %q = ANY($1) GROUP BY %q`,
		columnName, schemaName, tableName, columnName, columnName,
	)
	rows, err := p.pool.Query(ctx, sql, values)
	if err != nil {
		return nil, fmt.Errorf("table: count rows using values on %q.%q.%q: %w", schemaName, tableName, columnName, err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, fmt.Errorf("table: scan value count: %w", err)
		}
		if count > 0 {
			counts[value] = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("table: iterate value counts: %w", err)
	}
	return counts, nil
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
	if removed := removedValues(oldValues, newValues); len(removed) > 0 {
		counts, err := p.countRowsUsingValues(ctx, schemaName, tableName, columnName, removed)
		if err != nil {
			return err
		}
		if len(counts) > 0 {
			return &EnumValueInUseError{Column: columnName, Counts: counts}
		}
	}

	constraintName, found, err := p.findColumnCheckConstraint(ctx, schemaName, tableName, columnName)
	if err != nil {
		return err
	}

	addClause := fmt.Sprintf(`ADD CHECK (%q IN (%s))`, columnName, quotedValueList(newValues))
	var sql string
	if found {
		sql = fmt.Sprintf(`ALTER TABLE %q.%q DROP CONSTRAINT %q, %s`, schemaName, tableName, constraintName, addClause)
	} else {
		// No constraint to replace — converge by adding the missing one
		// rather than failing, the same way DropColumnForeignKey treats a
		// missing FK as a stale stored schema catching up to reality.
		sql = fmt.Sprintf(`ALTER TABLE %q.%q %s`, schemaName, tableName, addClause)
	}

	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("table: replace enum values on %q.%q.%q: %w", schemaName, tableName, columnName, err)
	}
	return nil
}

// Returns a list of changes in "schema.table.column (description)" format.
func (p *Provisioner) applyColumnChanges(ctx context.Context, schemaName, tableName string, cols []config.ColumnConfig, rls string) ([]string, error) {
	if err := p.ensureMigrationTable(ctx, schemaName); err != nil {
		return nil, fmt.Errorf("apply changes: ensure migration table: %w", err)
	}

	existing, err := p.fetchExistingColumns(ctx, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	var changes []string

	for _, col := range cols {
		if col.RenameFrom == "" {
			continue
		}
		if _, exists := existing[col.Name]; exists {
			continue
		}
		if _, exists := existing[col.RenameFrom]; !exists {
			continue
		}

		result, err := p.applyRename(ctx, schemaName, tableName, col, existing)
		if err != nil {
			return nil, err
		}
		if result != "" {
			changes = append(changes, result)
		}
	}

	existing, err = p.fetchExistingColumns(ctx, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	for _, col := range cols {
		result, err := p.applyTypeChange(ctx, schemaName, tableName, col, existing)
		if err != nil {
			return nil, err
		}
		if result != "" {
			changes = append(changes, result)
		}
	}

	return changes, nil
}
