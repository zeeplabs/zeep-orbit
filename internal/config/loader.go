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

// Load reads a YAML config file at path, interpolates env vars, unmarshals
// it into Config and validates all fields. Returns a non-nil error with a
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

// interpolateEnvVars replaces every ${VAR} token in s with os.Getenv("VAR").
// Returns an error if any referenced env var is not set.
func interpolateEnvVars(s string) (string, error) {
	var firstErr error
	result := envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		// Extract the variable name from ${VAR}
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
			}
		}
	}

	return nil
}
