package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	appNameRegex   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	tableNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	envVarRegex    = regexp.MustCompile(`\$\{([^}]+)\}`)

	validColumnTypes = map[string]bool{
		"text":        true,
		"integer":     true,
		"bigint":      true,
		"decimal":     true,
		"boolean":     true,
		"uuid":        true,
		"timestamptz": true,
		"jsonb":       true,
	}
)

// descriptive message on any failure.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read file %q: %w", path, err)
	}

	interpolated, err := interpolateEnvVars(string(raw))
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(interpolated), &cfg); err != nil {
		return nil, fmt.Errorf("config: YAML parse error: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Returns an error if any referenced env var is not set.
func interpolateEnvVars(s string) (string, error) {
	var firstErr error
	result := envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		name := match[2 : len(match)-1]
		val, ok := os.LookupEnv(name)
		if !ok {
			firstErr = fmt.Errorf("config: env var %s not set", name)
			return match
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}

// validate checks all structural and semantic constraints on a parsed Config.
func validate(cfg *Config) error {
	if strings.TrimSpace(cfg.Platform.DatabaseURL) == "" {
		return fmt.Errorf("config: platform.database_url is required")
	}

	appNames := make(map[string]bool)
	for i, app := range cfg.Apps {
		prefix := fmt.Sprintf("config: app[%d]", i)

		if strings.TrimSpace(app.Name) == "" {
			return fmt.Errorf("%s: name is required", prefix)
		}
		if !appNameRegex.MatchString(app.Name) {
			return fmt.Errorf("%s: name %q is invalid (must match ^[a-z][a-z0-9-]{0,62}$)", prefix, app.Name)
		}
		if appNames[app.Name] {
			return fmt.Errorf("config: duplicate app name %q", app.Name)
		}
		appNames[app.Name] = true

		if strings.TrimSpace(app.Auth.JWTSecret) == "" {
			return fmt.Errorf("%s (%s): auth.jwt_secret is required", prefix, app.Name)
		}

		if len(app.Tables) == 0 {
			return fmt.Errorf("%s (%s): at least one table is required", prefix, app.Name)
		}

		tableNames := make(map[string]bool)
		tablesByName := make(map[string]TableConfig, len(app.Tables))
		for _, table := range app.Tables {
			tablesByName[table.Name] = table
		}

		for j, table := range app.Tables {
			tPrefix := fmt.Sprintf("%s (%s), table[%d]", prefix, app.Name, j)

			if strings.TrimSpace(table.Name) == "" {
				return fmt.Errorf("%s: name is required", tPrefix)
			}
			if !tableNameRegex.MatchString(table.Name) {
				return fmt.Errorf("%s: name %q is invalid (must match ^[a-z][a-z0-9_]{0,62}$)", tPrefix, table.Name)
			}
			if tableNames[table.Name] {
				return fmt.Errorf("%s (%s): duplicate table name %q", prefix, app.Name, table.Name)
			}
			tableNames[table.Name] = true

			if len(table.Columns) == 0 {
				return fmt.Errorf("%s (%s): table %q must have at least one column", prefix, app.Name, table.Name)
			}

			for k, col := range table.Columns {
				cPrefix := fmt.Sprintf("%s (%s), table %q, column[%d]", prefix, app.Name, table.Name, k)

				if strings.TrimSpace(col.Name) == "" {
					return fmt.Errorf("%s: name is required", cPrefix)
				}
				if !validColumnTypes[col.Type] {
					return fmt.Errorf("%s (%s): column %q has unknown type %q", cPrefix, col.Name, col.Name, col.Type)
				}

				if col.References != nil {
					if err := validateReference(cPrefix, col, tablesByName); err != nil {
						return err
					}
				}
			}

			if err := validateIndexes(tPrefix, table); err != nil {
				return err
			}
		}

		if err := detectReferenceCycle(prefix, app.Tables); err != nil {
			return err
		}
	}

	return nil
}

var validOnDelete = map[string]bool{
	"":          true,
	"cascade":   true,
	"restrict":  true,
	"set_null":  true,
	"no_action": true,
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
		iPrefix := fmt.Sprintf("%s, index[%d]", tPrefix, i)

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
