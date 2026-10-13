package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// constraintTestTable creates an isolated schema with an "assets" table built
// through createTable (the same DDL path the dashboard uses), so constraint
// names and column shapes match production exactly.
func constraintTestTable(t *testing.T, prefix string, cols []config.ColumnConfig) (*db.Pool, string) {
	t.Helper()
	pool := ensureRLSTestPool(t)
	ctx := context.Background()
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
		pool.Close()
	})
	if _, err := New(pool).createTable(ctx, schema, "assets", cols, ""); err != nil {
		t.Fatalf("createTable: %v", err)
	}
	return pool, schema
}

func columnIsNotNull(t *testing.T, pool *db.Pool, schema, column string) bool {
	t.Helper()
	var nullable string
	if err := pool.QueryRow(context.Background(),
		`SELECT is_nullable FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'assets' AND column_name = $2`,
		schema, column,
	).Scan(&nullable); err != nil {
		t.Fatalf("read is_nullable for %q: %v", column, err)
	}
	return nullable == "NO"
}

func columnDefault(t *testing.T, pool *db.Pool, schema, column string) string {
	t.Helper()
	var def *string
	if err := pool.QueryRow(context.Background(),
		`SELECT column_default FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'assets' AND column_name = $2`,
		schema, column,
	).Scan(&def); err != nil {
		t.Fatalf("read column_default for %q: %v", column, err)
	}
	if def == nil {
		return ""
	}
	return *def
}

func uniqueConstraintCount(t *testing.T, pool *db.Pool, schema, column string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pg_constraint c
		 JOIN pg_class cl ON cl.oid = c.conrelid
		 JOIN pg_namespace n ON n.oid = cl.relnamespace
		 JOIN pg_attribute a ON a.attrelid = cl.oid AND a.attnum = c.conkey[1]
		 WHERE c.contype = 'u' AND n.nspname = $1 AND cl.relname = 'assets'
		   AND array_length(c.conkey, 1) = 1 AND a.attname = $2`,
		schema, column,
	).Scan(&n); err != nil {
		t.Fatalf("count unique constraints for %q: %v", column, err)
	}
	return n
}

func textCol(name string) config.ColumnConfig { return config.ColumnConfig{Name: name, Type: "text"} }

func TestDiffColumnConstraints_OnlyReportsChangedExistingColumns(t *testing.T) {
	old := []config.ColumnConfig{
		{Name: "serial", Type: "text", Required: true},
		{Name: "brand", Type: "text"},
		{Name: "code", Type: "text", Unique: true},
		{Name: "status", Type: "text", Default: "new"},
	}
	updated := []config.ColumnConfig{
		{Name: "serial", Type: "text"},                  // required true -> false
		{Name: "brand", Type: "text"},                   // unchanged
		{Name: "code", Type: "text", Unique: true},      // unchanged
		{Name: "status", Type: "text", Default: "used"}, // default changed
		{Name: "fresh", Type: "text", Required: true},   // brand-new column: addMissingColumns' job
		{Name: "created_at", Type: "timestamptz"},       // system column: never touched
	}

	changes := DiffColumnConstraints(old, updated)
	got := make([]string, 0, len(changes))
	for _, c := range changes {
		got = append(got, c.New.Name)
	}
	if strings.Join(got, ",") != "serial,status" {
		t.Fatalf("changed columns = %v, want [serial status]", got)
	}
}

// CCC-01: dropping "required" on an existing column must actually drop NOT
// NULL — the exact production drift (assets.serial) this feature fixes.
func TestApplyColumnConstraintChanges_DropRequired(t *testing.T) {
	old := config.ColumnConfig{Name: "serial", Type: "text", Required: true}
	pool, schema := constraintTestTable(t, "ccc_drop_required", []config.ColumnConfig{old})
	ctx := context.Background()

	updated := old
	updated.Required = false
	changes := []ColumnConstraintChange{{Old: old, New: updated}}
	p := New(pool)
	if err := p.CheckColumnConstraintChanges(ctx, schema, "assets", changes); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if columnIsNotNull(t, pool, schema, "serial") {
		t.Fatal("serial is still NOT NULL after dropping required")
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets DEFAULT VALUES`, schema)); err != nil {
		t.Fatalf("insert without serial must succeed now: %v", err)
	}
}

// CCC-01: setting "required" on a column with no NULLs applies SET NOT NULL.
func TestApplyColumnConstraintChanges_SetRequired(t *testing.T) {
	old := textCol("serial")
	pool, schema := constraintTestTable(t, "ccc_set_required", []config.ColumnConfig{old})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (serial) VALUES ('A1')`, schema)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated := old
	updated.Required = true
	changes := []ColumnConstraintChange{{Old: old, New: updated}}
	p := New(pool)
	if err := p.CheckColumnConstraintChanges(ctx, schema, "assets", changes); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !columnIsNotNull(t, pool, schema, "serial") {
		t.Fatal("serial should be NOT NULL after setting required")
	}
}

// CCC-04: rows with NULL block "required"; the check names the column and
// the exact row count, and nothing changes in the database.
func TestCheckColumnConstraintChanges_RequiredRejectedWhenNullsExist(t *testing.T) {
	old := textCol("serial")
	pool, schema := constraintTestTable(t, "ccc_required_nulls", []config.ColumnConfig{old})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets DEFAULT VALUES`, schema)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	updated := old
	updated.Required = true
	changes := []ColumnConstraintChange{{Old: old, New: updated}}
	p := New(pool)

	for name, run := range map[string]func() error{
		"check": func() error { return p.CheckColumnConstraintChanges(ctx, schema, "assets", changes) },
		"apply": func() error { _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", changes); return err },
	} {
		err := run()
		var ccErr *ColumnConstraintError
		if !errors.As(err, &ccErr) {
			t.Fatalf("%s: expected *ColumnConstraintError, got %T (%v)", name, err, err)
		}
		if ccErr.Column != "serial" || ccErr.Reason != ReasonNullValues || ccErr.Count != 2 {
			t.Fatalf("%s: got %+v, want serial/null_values/2", name, ccErr)
		}
		if !strings.Contains(err.Error(), `"serial"`) || !strings.Contains(err.Error(), "2 existing row(s)") {
			t.Fatalf("%s: message must name column and count, got %q", name, err.Error())
		}
	}
	if columnIsNotNull(t, pool, schema, "serial") {
		t.Fatal("a rejected change must not alter the column")
	}
}

// CCC-02: literal default set, then dropped.
func TestApplyColumnConstraintChanges_SetAndDropDefault(t *testing.T) {
	old := textCol("status")
	pool, schema := constraintTestTable(t, "ccc_default", []config.ColumnConfig{old})
	ctx := context.Background()
	p := New(pool)

	withDefault := old
	withDefault.Default = "it's new"
	if _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: old, New: withDefault}}); err != nil {
		t.Fatalf("set default: %v", err)
	}
	var got string
	if err := pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %q.assets DEFAULT VALUES RETURNING status`, schema)).Scan(&got); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got != "it's new" {
		t.Fatalf("default not applied: got %q", got)
	}

	if _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: withDefault, New: old}}); err != nil {
		t.Fatalf("drop default: %v", err)
	}
	if d := columnDefault(t, pool, schema, "status"); d != "" {
		t.Fatalf("default should be dropped, got %q", d)
	}
}

// CCC-02: expression default (allowlisted by config.ValidateTables).
func TestApplyColumnConstraintChanges_ExpressionDefault(t *testing.T) {
	old := config.ColumnConfig{Name: "synced_at", Type: "timestamptz"}
	pool, schema := constraintTestTable(t, "ccc_expr_default", []config.ColumnConfig{old})
	ctx := context.Background()

	updated := old
	updated.Default = "now()"
	updated.DefaultIsExpression = true
	if _, err := New(pool).ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: old, New: updated}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if d := columnDefault(t, pool, schema, "synced_at"); d != "now()" {
		t.Fatalf("column_default = %q, want now()", d)
	}
}

// CCC-03 + CCC-06: unique added once (re-applying does not create a second
// constraint), then removed.
func TestApplyColumnConstraintChanges_AddAndRemoveUnique(t *testing.T) {
	old := textCol("code")
	pool, schema := constraintTestTable(t, "ccc_unique", []config.ColumnConfig{old})
	ctx := context.Background()
	p := New(pool)

	unique := old
	unique.Unique = true
	add := []ColumnConstraintChange{{Old: old, New: unique}}
	for i := 0; i < 2; i++ {
		if _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", add); err != nil {
			t.Fatalf("add unique (#%d): %v", i+1, err)
		}
	}
	if n := uniqueConstraintCount(t, pool, schema, "code"); n != 1 {
		t.Fatalf("unique constraints on code = %d, want exactly 1", n)
	}

	if _, err := p.ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: unique, New: old}}); err != nil {
		t.Fatalf("remove unique: %v", err)
	}
	if n := uniqueConstraintCount(t, pool, schema, "code"); n != 0 {
		t.Fatalf("unique constraints on code = %d, want 0", n)
	}
}

// CCC-04: duplicated values block "unique".
func TestCheckColumnConstraintChanges_UniqueRejectedWhenDuplicatesExist(t *testing.T) {
	old := textCol("code")
	pool, schema := constraintTestTable(t, "ccc_unique_dupes", []config.ColumnConfig{old})
	ctx := context.Background()
	for _, v := range []string{"A", "A", "B", "B", "C"} {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (code) VALUES ($1)`, schema), v); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// NULLs never conflict under a UNIQUE constraint and must not be counted.
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets DEFAULT VALUES`, schema)); err != nil {
			t.Fatalf("seed null: %v", err)
		}
	}

	unique := old
	unique.Unique = true
	err := New(pool).CheckColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: old, New: unique}})
	var ccErr *ColumnConstraintError
	if !errors.As(err, &ccErr) {
		t.Fatalf("expected *ColumnConstraintError, got %T (%v)", err, err)
	}
	if ccErr.Reason != ReasonDuplicateValues || ccErr.Count != 2 {
		t.Fatalf("got %+v, want duplicate_values/2", ccErr)
	}
}

// CCC-05: a failing change rolls back the changes before it in the same call.
func TestApplyColumnConstraintChanges_AllOrNothing(t *testing.T) {
	a := config.ColumnConfig{Name: "serial", Type: "text", Required: true}
	b := textCol("code")
	pool, schema := constraintTestTable(t, "ccc_atomic", []config.ColumnConfig{a, b})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (serial) VALUES ('x')`, schema)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	aNew := a
	aNew.Required = false
	bNew := b
	bNew.Required = true // code is NULL on the seeded row -> must fail
	_, err := New(pool).ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: a, New: aNew}, {Old: b, New: bNew}})
	var ccErr *ColumnConstraintError
	if !errors.As(err, &ccErr) {
		t.Fatalf("expected *ColumnConstraintError, got %T (%v)", err, err)
	}
	if !columnIsNotNull(t, pool, schema, "serial") {
		t.Fatal("serial's DROP NOT NULL must be rolled back when a later change in the same call fails")
	}
}

// CCC-06: re-dropping NOT NULL on an already-nullable column (metadata had
// drifted the other way) is a harmless no-op, not an error.
func TestApplyColumnConstraintChanges_IdempotentAgainstDrift(t *testing.T) {
	col := textCol("serial")
	pool, schema := constraintTestTable(t, "ccc_idempotent", []config.ColumnConfig{col})
	ctx := context.Background()

	stale := col
	stale.Required = true // metadata claimed required, database never was
	if _, err := New(pool).ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: stale, New: col}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if columnIsNotNull(t, pool, schema, "serial") {
		t.Fatal("serial should remain nullable")
	}
}

// CCC-04: adding a brand-new required column without a default to a table
// that already has rows is rejected with a typed error naming the row count,
// instead of the raw 23502 that used to surface as "provisioning failed".
func TestAddMissingColumns_RequiredWithoutDefaultOnNonEmptyTable(t *testing.T) {
	base := textCol("serial")
	pool, schema := constraintTestTable(t, "ccc_add_required", []config.ColumnConfig{base})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (serial) VALUES ('x')`, schema)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cols := []config.ColumnConfig{base, {Name: "owner_email", Type: "text", Required: true}}
	_, err := New(pool).addMissingColumns(ctx, schema, "assets", cols, "")
	var ccErr *ColumnConstraintError
	if !errors.As(err, &ccErr) {
		t.Fatalf("expected *ColumnConstraintError, got %T (%v)", err, err)
	}
	if ccErr.Column != "owner_email" || ccErr.Reason != ReasonRequiredWithoutDefault || ccErr.Count != 1 {
		t.Fatalf("got %+v, want owner_email/required_without_default/1", ccErr)
	}

	// With a default, the same column is accepted (Postgres backfills it).
	cols[1].Default = "unknown@example.com"
	if _, err := New(pool).addMissingColumns(ctx, schema, "assets", cols, ""); err != nil {
		t.Fatalf("required column with default must be accepted: %v", err)
	}
}

// CCC-04: removing "unique" from a column a foreign key depends on is a
// typed rejection, not a raw 2BP01 surfacing as a generic 500.
func TestApplyColumnConstraintChanges_RemoveUniqueReferencedByForeignKey(t *testing.T) {
	code := config.ColumnConfig{Name: "code", Type: "text", Unique: true}
	pool, schema := constraintTestTable(t, "ccc_unique_fk", []config.ColumnConfig{code})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.loans (asset_code TEXT REFERENCES %q.assets(code))`, schema, schema)); err != nil {
		t.Fatalf("create referencing table: %v", err)
	}

	notUnique := code
	notUnique.Unique = false
	_, err := New(pool).ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: code, New: notUnique}})
	var ccErr *ColumnConstraintError
	if !errors.As(err, &ccErr) || ccErr.Reason != ReasonUniqueReferenced {
		t.Fatalf("expected unique_referenced *ColumnConstraintError, got %T (%v)", err, err)
	}
	if n := uniqueConstraintCount(t, pool, schema, "code"); n != 1 {
		t.Fatalf("unique constraint must survive a rejected removal, got %d", n)
	}
}
