package provisioner_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestGrantEnduserAccess_NewSchemaNewTable covers ROWPOL-13: a brand-new app
// (new schema) provisioned with a table must let zeep_app_enduser
// SELECT/INSERT/UPDATE/DELETE on a table created afterward in that schema —
// proving the ALTER DEFAULT PRIVILEGES path covers future tables, not just
// the ones that existed at schema-creation time.
func TestGrantEnduserAccess_NewSchemaNewTable(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	schema := uniqueSchema("test_grant_new")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{Name: schema, Tables: nil},
		},
	}
	// First Apply: only creates the schema (no tables yet) — this is where
	// GRANT USAGE + ALTER DEFAULT PRIVILEGES get set up.
	if _, err := prov.Apply(ctx, cfg); err != nil {
		t.Fatalf("first Apply (schema only): %v", err)
	}

	// Second Apply, now with a table: the table is created AFTER the schema's
	// default privileges were set, simulating "app created, table added later".
	cfg.Apps[0].Tables = []config.TableConfig{
		{
			Name: "widgets",
			Columns: []config.ColumnConfig{
				{Name: "label", Type: "text", Required: true},
			},
		},
	}
	if _, err := prov.Apply(ctx, cfg); err != nil {
		t.Fatalf("second Apply (with table): %v", err)
	}

	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var granted bool
		err := pool.QueryRow(ctx,
			`SELECT has_table_privilege('zeep_app_enduser', $1, $2)`,
			fmt.Sprintf("%s.widgets", schema), priv,
		).Scan(&granted)
		if err != nil {
			t.Fatalf("has_table_privilege(%s): %v", priv, err)
		}
		if !granted {
			t.Errorf("expected zeep_app_enduser to have %s on %s.widgets (via default privileges), got false", priv, schema)
		}
	}
}

// TestGrantEnduserAccess_ExistingSchemaExistingTable covers the other half of
// ROWPOL-13's Done-when: an app schema/table that already existed before this
// feature's grant step ran (simulated here with a raw CREATE TABLE bypassing
// the provisioner) must still end up granted to zeep_app_enduser after Apply
// runs — no app silently excluded from the migration pass.
func TestGrantEnduserAccess_ExistingSchemaExistingTable(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	schema := uniqueSchema("test_grant_existing")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	// Simulate a pre-feature app: schema + table created directly, with no
	// grant to zeep_app_enduser at all (as if it predates this feature).
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %q."legacy_items" (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT)`, schema,
	)); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	// Sanity check: before the migration pass, zeep_app_enduser has no access.
	var grantedBefore bool
	if err := pool.QueryRow(ctx,
		`SELECT has_table_privilege('zeep_app_enduser', $1, 'SELECT')`,
		fmt.Sprintf("%s.legacy_items", schema),
	).Scan(&grantedBefore); err != nil {
		// zeep_app_enduser may not exist yet in a fresh test DB — that itself
		// means "no access", consistent with what we're about to prove wrong.
		grantedBefore = false
	}
	if grantedBefore {
		t.Fatalf("expected no pre-migration access to legacy_items, got granted=true")
	}

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{Name: schema, Tables: nil},
		},
	}
	if _, err := prov.Apply(ctx, cfg); err != nil {
		t.Fatalf("Apply on existing schema: %v", err)
	}

	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var granted bool
		err := pool.QueryRow(ctx,
			`SELECT has_table_privilege('zeep_app_enduser', $1, $2)`,
			fmt.Sprintf("%s.legacy_items", schema), priv,
		).Scan(&granted)
		if err != nil {
			t.Fatalf("has_table_privilege(%s): %v", priv, err)
		}
		if !granted {
			t.Errorf("expected zeep_app_enduser to have %s on pre-existing %s.legacy_items after migration pass, got false", priv, schema)
		}
	}
}
