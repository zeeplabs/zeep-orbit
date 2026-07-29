package config

import (
	"fmt"
	"strings"
)

var validOnDelete = map[string]bool{
	"":          true,
	"cascade":   true,
	"restrict":  true,
	"set_null":  true,
	"no_action": true,
}

// ValidateTables checks foreign key references, index declarations, and
// circular dependencies across a single app's full set of tables. Callers
// (currently the dashboard's table create/update handlers) must pass the
// complete desired table set — the table being created/edited plus all of
// the app's other tables — since references and cycles can only be checked
// against the whole set.
func ValidateTables(tables []TableConfig) error {
	tablesByName := make(map[string]TableConfig, len(tables))
	for _, t := range tables {
		tablesByName[t.Name] = t
	}

	for _, table := range tables {
		for k, col := range table.Columns {
			if col.References == nil {
				continue
			}
			cPrefix := fmt.Sprintf("table %q, column[%d] (%s)", table.Name, k, col.Name)
			if err := validateReference(cPrefix, col, tablesByName); err != nil {
				return err
			}
		}

		if err := validateIndexes(table.Name, table); err != nil {
			return err
		}
	}

	return detectReferenceCycle("schema", tables)
}

// validateReference checks a single column's References against the app's
// own tables (cross-app references are out of scope — schema-per-app
// isolation is an existing architectural boundary).
func validateReference(cPrefix string, col ColumnConfig, tablesByName map[string]TableConfig) error {
	ref := col.References
	if strings.TrimSpace(ref.Table) == "" {
		return fmt.Errorf("%s: references.table is required", cPrefix)
	}
	target, ok := tablesByName[ref.Table]
	if !ok {
		return fmt.Errorf("%s: references unknown table %q", cPrefix, ref.Table)
	}

	if strings.TrimSpace(ref.Column) == "" {
		return fmt.Errorf("%s: references.column is required", cPrefix)
	}
	if ref.Column != "id" {
		found := false
		for _, tc := range target.Columns {
			if tc.Name == ref.Column && tc.Unique {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: references %q.%q, which is not %q's primary key and not declared unique", cPrefix, ref.Table, ref.Column, ref.Table)
		}
	}

	if !validOnDelete[ref.OnDelete] {
		return fmt.Errorf("%s: references.on_delete %q is invalid (must be cascade, restrict, set_null or no_action)", cPrefix, ref.OnDelete)
	}
	if ref.OnDelete == "set_null" && col.Required {
		return fmt.Errorf("%s: references.on_delete=set_null is incompatible with required=true", cPrefix)
	}

	return nil
}

// validateIndexes checks that every declared index references existing
// columns and that index names are unique within the table's app (indexes
// live in the same PostgreSQL schema, so names must not collide app-wide).
func validateIndexes(tPrefix string, table TableConfig) error {
	colNames := make(map[string]bool, len(table.Columns))
	for _, c := range table.Columns {
		colNames[c.Name] = true
	}

	for i, idx := range table.Indexes {
		iPrefix := fmt.Sprintf("table %q, index[%d]", tPrefix, i)

		if strings.TrimSpace(idx.Name) == "" {
			return fmt.Errorf("%s: name is required", iPrefix)
		}
		if len(idx.Columns) == 0 {
			return fmt.Errorf("%s (%s): must declare at least one column", iPrefix, idx.Name)
		}
		for _, c := range idx.Columns {
			if !colNames[c] {
				return fmt.Errorf("%s (%s): column %q does not exist on table %q", iPrefix, idx.Name, c, table.Name)
			}
		}
	}

	return nil
}

// detectReferenceCycle walks the table dependency graph (table -> tables it
// references) within a single app and rejects any cycle before DDL runs.
func detectReferenceCycle(prefix string, tables []TableConfig) error {
	deps := make(map[string][]string, len(tables))
	for _, t := range tables {
		for _, c := range t.Columns {
			// Self-references (e.g. manager_id -> same table) don't need
			// ordering against anything else, so they're not a graph edge.
			if c.References != nil && c.References.Table != t.Name {
				deps[t.Name] = append(deps[t.Name], c.References.Table)
			}
		}
	}

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(tables))
	var path []string

	var visit func(node string) error
	visit = func(node string) error {
		switch state[node] {
		case visited:
			return nil
		case visiting:
			path = append(path, node)
			return fmt.Errorf("%s: circular foreign key dependency: %s", prefix, strings.Join(path, " -> "))
		}
		state[node] = visiting
		path = append(path, node)
		for _, dep := range deps[node] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[node] = visited
		return nil
	}

	for _, t := range tables {
		if state[t.Name] == unvisited {
			path = nil
			if err := visit(t.Name); err != nil {
				return err
			}
		}
	}

	return nil
}
