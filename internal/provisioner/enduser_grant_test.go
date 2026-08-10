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

// TestBackfillEnduserGrants_PreExistingSchemaWithoutApply covers the boot
// path: an app schema that was created before end-user-row-policies shipped
// and has NEVER gone through provisioner.Apply again (no edit through the
// Dashboard since upgrading) must still get zeep_app_enduser access once
// BackfillEnduserGrants runs — this is what cmd/zeep's serve command calls
// once at boot for every app already in the registry, so upgrading doesn't
// silently break end-user requests on untouched apps (see
// db.Pool.WithRLSContext, which now always SET LOCAL ROLEs into
// zeep_app_enduser for the data plane).
func TestBackfillEnduserGrants_PreExistingSchemaWithoutApply(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	schema := uniqueSchema("test_backfill")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %q."legacy_items" (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT)`, schema,
	)); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	prov := provisioner.New(pool)
	if errs := prov.BackfillEnduserGrants(ctx, []string{schema}); len(errs) != 0 {
		t.Fatalf("BackfillEnduserGrants: %v", errs)
	}

	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var granted bool
		if err := pool.QueryRow(ctx,
			`SELECT has_table_privilege('zeep_app_enduser', $1, $2)`,
			fmt.Sprintf("%s.legacy_items", schema), priv,
		).Scan(&granted); err != nil {
			t.Fatalf("has_table_privilege(%s): %v", priv, err)
		}
		if !granted {
			t.Errorf("expected zeep_app_enduser to have %s on %s.legacy_items after backfill, got false", priv, schema)
		}
	}
}

// TestBackfillEnduserGrants_OneBadSchemaDoesNotBlockOthers proves a schema
// that no longer exists (e.g. app deleted from disk but not yet from the
// registry) is reported as an error without stopping the grant from running
// on the other schemas passed in — a single bad app must not leave every
// other app ungranted at boot.
func TestBackfillEnduserGrants_OneBadSchemaDoesNotBlockOthers(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	good := uniqueSchema("test_backfill_good")
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, good)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { dropSchema(t, pool, good) })

	prov := provisioner.New(pool)
	errs := prov.BackfillEnduserGrants(ctx, []string{"schema_that_does_not_exist_at_all", good})
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error for the missing schema, got %d: %v", len(errs), errs)
	}

	var granted bool
	if err := pool.QueryRow(ctx,
		`SELECT has_schema_privilege('zeep_app_enduser', $1, 'USAGE')`, good,
	).Scan(&granted); err != nil {
		t.Fatalf("has_schema_privilege: %v", err)
	}
	if !granted {
		t.Error("expected the good schema to still be granted despite the other schema failing")
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
