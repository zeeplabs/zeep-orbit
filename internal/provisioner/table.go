package provisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// and should not be defined by the user.
var systemColumnNames = map[string]bool{
	"id":         true,
	"created_at": true,
	"updated_at": true,
	"owner_id":   true,
}

// pgType converte o tipo do config para o tipo PostgreSQL correspondente.
func pgType(t string) string {
	switch t {
	case "text":
		return "TEXT"
	case "integer":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "decimal":
		return "DECIMAL"
	case "boolean":
		return "BOOLEAN"
	case "uuid":
		return "UUID"
	case "timestamptz":
		return "TIMESTAMPTZ"
	case "jsonb":
		return "JSONB"
	default:
		return "TEXT"
	}
}

// Single quotes in DEFAULT value are escaped by doubling them ('').
func columnDDL(col config.ColumnConfig) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%q %s", col.Name, pgType(col.Type)))

	if col.Required {
		sb.WriteString(" NOT NULL")
	}
	if col.Default != "" {
		escaped := strings.ReplaceAll(col.Default, "'", "''")
		sb.WriteString(fmt.Sprintf(" DEFAULT '%s'", escaped))
	}
	if col.Unique {
		sb.WriteString(" UNIQUE")
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
		colDefs = append(colDefs, columnDDL(col))
	}

	if rls == "owner" || rls == "enabled" {
		colDefs = append(colDefs, fmt.Sprintf(`"owner_id" UUID NOT NULL REFERENCES %q."_auth_users"("id")`, schemaName))
	}

	colDefs = append(colDefs,
		`"created_at" TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`"updated_at" TIMESTAMPTZ NOT NULL DEFAULT now()`,
	)

	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %q.%q (%s)`,
		schemaName, tableName,
		strings.Join(colDefs, ", "),
	)

	if _, err := p.pool.Exec(ctx, sql); err != nil {
		return false, fmt.Errorf("table: create %q.%q: %w", schemaName, tableName, err)
	}

	return true, nil
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
			columnDDL(col),
		)
		if _, err := p.pool.Exec(ctx, sql); err != nil {
			return nil, fmt.Errorf("table: add column %q to %q.%q: %w", col.Name, schemaName, tableName, err)
		}

		added = append(added, fmt.Sprintf("%s.%s.%s", schemaName, tableName, col.Name))
	}

	if rls == "owner" || rls == "enabled" {
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

// fetchExistingColumns retorna um map[nomeColuna]udtName das colunas atuais de uma tabela.
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

