package config

import (
	"os"
	"path/filepath"
	"testing"
)

// its path. The file is cleaned up when the test ends.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	return path
}

const validYAML = `
platform:
  database_url: "postgres://user:pass@localhost:5432/testdb"
apps:
  - name: my-app
    auth:
      jwt_secret: "supersecret"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
            required: true
          - name: email
            type: text
            required: true
            unique: true
          - name: created_at
            type: timestamptz
`

func TestLoadValid(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Platform.DatabaseURL != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("unexpected database_url: %q", cfg.Platform.DatabaseURL)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(cfg.Apps))
	}
	app := cfg.Apps[0]
	if app.Name != "my-app" {
		t.Errorf("unexpected app name: %q", app.Name)
	}
	if app.Auth.JWTSecret != "supersecret" {
		t.Errorf("unexpected jwt_secret: %q", app.Auth.JWTSecret)
	}
	if len(app.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(app.Tables))
	}
	if app.Tables[0].Name != "users" {
		t.Errorf("unexpected table name: %q", app.Tables[0].Name)
	}
	if len(app.Tables[0].Columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(app.Tables[0].Columns))
	}
}

func TestLoadMissingRequired(t *testing.T) {
	t.Run("missing jwt_secret", func(t *testing.T) {
		t.Helper()
		content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: my-app
    auth:
      jwt_secret: ""
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
`
		path := writeYAML(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for empty jwt_secret, got nil")
		}
	})

	t.Run("missing database_url", func(t *testing.T) {
		t.Helper()
		content := `
platform:
  database_url: ""
apps:
  - name: my-app
    auth:
      jwt_secret: "secret"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
`
		path := writeYAML(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for empty database_url, got nil")
		}
	})

	t.Run("no tables", func(t *testing.T) {
		t.Helper()
		content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: my-app
    auth:
      jwt_secret: "secret"
    tables: []
`
		path := writeYAML(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for empty tables, got nil")
		}
	})
}

func TestLoadInvalidName(t *testing.T) {
	t.Run("app name with uppercase", func(t *testing.T) {
		t.Helper()
		content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: MyApp
    auth:
      jwt_secret: "secret"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
`
		path := writeYAML(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for uppercase app name, got nil")
		}
	})

	t.Run("app name with space", func(t *testing.T) {
		t.Helper()
		content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: "my app"
    auth:
      jwt_secret: "secret"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
`
		path := writeYAML(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for app name with space, got nil")
		}
	})

	t.Run("table name with uppercase", func(t *testing.T) {
		t.Helper()
		content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: my-app
    auth:
      jwt_secret: "secret"
    tables:
      - name: Users
        columns:
          - name: id
            type: uuid
`
		path := writeYAML(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for uppercase table name, got nil")
		}
	})
}

func TestLoadUnknownType(t *testing.T) {
	content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: my-app
    auth:
      jwt_secret: "secret"
    tables:
      - name: users
        columns:
          - name: name
            type: varchar
`
	path := writeYAML(t, content)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown column type varchar, got nil")
	}
}

func TestLoadDuplicateApp(t *testing.T) {
	content := `
platform:
  database_url: "postgres://localhost/db"
apps:
  - name: my-app
    auth:
      jwt_secret: "secret"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
  - name: my-app
    auth:
      jwt_secret: "another-secret"
    tables:
      - name: orders
        columns:
          - name: id
            type: uuid
`
	path := writeYAML(t, content)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate app name, got nil")
	}
}

func TestLoadEnvVarInterpolation(t *testing.T) {
	t.Setenv("TEST_DB_URL", "postgres://envuser:envpass@localhost/envdb")
	t.Setenv("TEST_JWT", "env-jwt-secret")

	content := `
platform:
  database_url: "${TEST_DB_URL}"
apps:
  - name: my-app
    auth:
      jwt_secret: "${TEST_JWT}"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
`
	path := writeYAML(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Platform.DatabaseURL != "postgres://envuser:envpass@localhost/envdb" {
		t.Errorf("unexpected database_url after interpolation: %q", cfg.Platform.DatabaseURL)
	}
	if cfg.Apps[0].Auth.JWTSecret != "env-jwt-secret" {
		t.Errorf("unexpected jwt_secret after interpolation: %q", cfg.Apps[0].Auth.JWTSecret)
	}
}

func TestLoadEnvVarMissing(t *testing.T) {
	os.Unsetenv("MISSING_VAR") //nolint:errcheck

	content := `
platform:
  database_url: "${MISSING_VAR}"
apps:
  - name: my-app
    auth:
      jwt_secret: "secret"
    tables:
      - name: users
        columns:
          - name: id
            type: uuid
`
	path := writeYAML(t, content)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing env var, got nil")
	}
}
