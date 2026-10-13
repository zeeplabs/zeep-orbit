package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ColumnConstraintChange is one existing column's requested change to
// required/default/unique: Old is what the stored metadata said before the
// request, New what the request asks for. Type, enum values and references
// are not read here — each has its own dedicated path (applyTypeChange,
// ReplaceColumnEnumValues, Add/DropColumnForeignKey).
type ColumnConstraintChange struct {
	Old config.ColumnConfig
	New config.ColumnConfig
}

func (c ColumnConstraintChange) requiredChanged() bool { return c.Old.Required != c.New.Required }
func (c ColumnConstraintChange) uniqueChanged() bool   { return c.Old.Unique != c.New.Unique }
func (c ColumnConstraintChange) defaultChanged() bool {
	return c.Old.Default != c.New.Default || c.Old.DefaultIsExpression != c.New.DefaultIsExpression
}

// DiffColumnConstraints returns, in newCols order, one change for every
// column present in both oldCols and newCols whose required, default or
// unique differ. Brand-new columns are addMissingColumns' job and system
// columns are never user-managed, so neither is reported.
//
// The diff is deliberately metadata-vs-metadata (the user's explicit intent
// in this request), never metadata-vs-database: diffing against the database
// would turn any unrelated table save into surprise DDL on a column whose
// metadata had silently drifted in the past.
func DiffColumnConstraints(oldCols, newCols []config.ColumnConfig) []ColumnConstraintChange {
	before := make(map[string]config.ColumnConfig, len(oldCols))
	for _, c := range oldCols {
		before[c.Name] = c
	}
	var changes []ColumnConstraintChange
	for _, c := range newCols {
		if systemColumnNames[c.Name] {
			continue
		}
		old, existed := before[c.Name]
		if !existed {
			continue
		}
		ch := ColumnConstraintChange{Old: old, New: c}
		if ch.requiredChanged() || ch.defaultChanged() || ch.uniqueChanged() {
			changes = append(changes, ch)
		}
	}
	return changes
}

// CheckColumnConstraintChanges is the read-only pre-check for
// ApplyColumnConstraintChanges: it returns a *ColumnConstraintError when
// existing data rules a change out (NULLs for "required", duplicates for
// "unique") without running any DDL. Callers run it before any other schema
// change in the same request, so a rejection leaves the table untouched.
func (p *Provisioner) CheckColumnConstraintChanges(ctx context.Context, schemaName, tableName string, changes []ColumnConstraintChange) error {
	for _, ch := range changes {
		if err := checkColumnConstraintChange(ctx, p.pool, schemaName, tableName, ch); err != nil {
			return err
		}
	}
	return nil
}

func checkColumnConstraintChange(ctx context.Context, q db.Querier, schemaName, tableName string, ch ColumnConstraintChange) error {
	col := ch.New.Name
	if ch.requiredChanged() && ch.New.Required {
		var nulls int64
		sql := fmt.Sprintf(`SELECT COUNT(*) FROM %q.%q WHERE %q IS NULL`, schemaName, tableName, col)
		if err := q.QueryRow(ctx, sql).Scan(&nulls); err != nil {
			return fmt.Errorf("table: count NULLs in %q.%q.%q: %w", schemaName, tableName, col, err)
		}
		if nulls > 0 {
			return &ColumnConstraintError{Column: col, Reason: ReasonNullValues, Count: nulls}
		}
	}
	if ch.uniqueChanged() && ch.New.Unique {
		var dupes int64
		sql := fmt.Sprintf(
			`SELECT COUNT(*) FROM (SELECT 1 FROM %q.%q WHERE %q IS NOT NULL GROUP BY %q HAVING COUNT(*) > 1) d`,
			schemaName, tableName, col, col,
		)
		if err := q.QueryRow(ctx, sql).Scan(&dupes); err != nil {
			return fmt.Errorf("table: count duplicates in %q.%q.%q: %w", schemaName, tableName, col, err)
		}
		if dupes > 0 {
			return &ColumnConstraintError{Column: col, Reason: ReasonDuplicateValues, Count: dupes}
		}
	}
	return nil
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
	if len(changes) == 0 {
		return nil, nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("table: begin constraint changes on %q.%q: %w", schemaName, tableName, err)
	}
	defer tx.Rollback(ctx)

	var applied []string
	for _, ch := range changes {
		if err := checkColumnConstraintChange(ctx, tx, schemaName, tableName, ch); err != nil {
			return nil, err
		}
		col := ch.New.Name
		prefix := fmt.Sprintf("%s.%s.%s", schemaName, tableName, col)

		if ch.requiredChanged() {
			action, desc := "DROP NOT NULL", "required removed"
			if ch.New.Required {
				action, desc = "SET NOT NULL", "required set"
			}
			sql := fmt.Sprintf(`ALTER TABLE %q.%q ALTER COLUMN %q %s`, schemaName, tableName, col, action)
			if _, err := tx.Exec(ctx, sql); err != nil {
				return nil, constraintDDLError(col, err)
			}
			applied = append(applied, fmt.Sprintf("%s (%s)", prefix, desc))
		}

		if ch.defaultChanged() {
			action, desc := "DROP DEFAULT", "default removed"
			if ch.New.Default != "" {
				action, desc = "SET DEFAULT "+defaultSQL(ch.New), "default set"
			}
			sql := fmt.Sprintf(`ALTER TABLE %q.%q ALTER COLUMN %q %s`, schemaName, tableName, col, action)
			if _, err := tx.Exec(ctx, sql); err != nil {
				return nil, constraintDDLError(col, err)
			}
			applied = append(applied, fmt.Sprintf("%s (%s)", prefix, desc))
		}

		if ch.uniqueChanged() {
			names, err := singleColumnUniqueConstraints(ctx, tx, schemaName, tableName, col)
			if err != nil {
				return nil, err
			}
			if ch.New.Unique {
				if len(names) == 0 {
					// Left unnamed so Postgres applies its "<table>_<column>_key"
					// convention — identical to a column created with inline
					// UNIQUE (columnDDL).
					sql := fmt.Sprintf(`ALTER TABLE %q.%q ADD UNIQUE (%q)`, schemaName, tableName, col)
					if _, err := tx.Exec(ctx, sql); err != nil {
						return nil, constraintDDLError(col, err)
					}
				}
				applied = append(applied, fmt.Sprintf("%s (unique set)", prefix))
			} else {
				for _, name := range names {
					sql := fmt.Sprintf(`ALTER TABLE %q.%q DROP CONSTRAINT %q`, schemaName, tableName, name)
					if _, err := tx.Exec(ctx, sql); err != nil {
						return nil, constraintDDLError(col, err)
					}
				}
				applied = append(applied, fmt.Sprintf("%s (unique removed)", prefix))
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("table: commit constraint changes on %q.%q: %w", schemaName, tableName, err)
	}
	return applied, nil
}

// singleColumnUniqueConstraints lists the UNIQUE constraints covering
// exactly this one column. Multi-column uniques and plain unique indexes are
// managed through table indexes, not the column's "unique" flag, and are
// never touched here.
func singleColumnUniqueConstraints(ctx context.Context, q db.Querier, schemaName, tableName, column string) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT c.conname FROM pg_constraint c
		 JOIN pg_class cl ON cl.oid = c.conrelid
		 JOIN pg_namespace n ON n.oid = cl.relnamespace
		 JOIN pg_attribute a ON a.attrelid = cl.oid AND a.attnum = c.conkey[1]
		 WHERE c.contype = 'u' AND n.nspname = $1 AND cl.relname = $2
		   AND array_length(c.conkey, 1) = 1 AND a.attname = $3
		 ORDER BY c.conname`,
		schemaName, tableName, column,
	)
	if err != nil {
		return nil, fmt.Errorf("table: list unique constraints on %q.%q.%q: %w", schemaName, tableName, column, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("table: scan unique constraint name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// constraintDDLError maps a Postgres failure from one of the ALTER COLUMN /
// ADD|DROP CONSTRAINT statements above to a *ColumnConstraintError when the
// cause is the user's data or schema, so callers can return it as a 400.
// Anything else stays a wrapped internal error.
func constraintDDLError(column string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23502":
			return &ColumnConstraintError{Column: column, Reason: ReasonNullValues, Cause: err}
		case pgErr.Code == "23505":
			return &ColumnConstraintError{Column: column, Reason: ReasonDuplicateValues, Cause: err}
		case pgErr.Code == "2BP01":
			return &ColumnConstraintError{Column: column, Reason: ReasonUniqueReferenced, Cause: err}
		case strings.HasPrefix(pgErr.Code, "22"), pgErr.Code == "42804", pgErr.Code == "42846":
			return &ColumnConstraintError{Column: column, Reason: ReasonInvalidDefault, Cause: err}
		}
	}
	return fmt.Errorf("table: change constraints of %q: %w", column, err)
}
