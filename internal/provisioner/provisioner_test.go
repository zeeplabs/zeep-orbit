package provisioner_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

func testPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	return pool
}

// uniqueSchema generates a unique schema name per test to avoid collisions.
func uniqueSchema(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// dropSchema limpa o schema de teste ao final.
func dropSchema(t *testing.T, pool *db.Pool, schema string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema),
	)
	if err != nil {
		t.Logf("warn: cleanup drop schema %q: %v", schema, err)
	}
}

func TestCreateSchema(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_prov")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{Name: schema, Tables: nil},
		},
	}

	report, err := prov.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(report.SchemasCreated) != 1 || report.SchemasCreated[0] != schema {
		t.Errorf("expected SchemasCreated=[%q], got %v", schema, report.SchemasCreated)
	}

	// Confirma no pg_namespace.
	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("pg_namespace query: %v", err)
	}
	if !exists {
		t.Errorf("schema %q not found in pg_namespace after Apply", schema)
	}
}

func TestCreateTable(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_prov")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "users",
						Columns: []config.ColumnConfig{
							{Name: "name", Type: "text", Required: true},
							{Name: "age", Type: "integer"},
						},
					},
				},
			},
		},
	}

	report, err := prov.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	expectedTable := fmt.Sprintf("%s.users", schema)
	if len(report.TablesCreated) != 1 || report.TablesCreated[0] != expectedTable {
		t.Errorf("expected TablesCreated=[%q], got %v", expectedTable, report.TablesCreated)
	}

	systemCols := []string{"id", "created_at", "updated_at"}
	for _, col := range systemCols {
		var exists bool
		err = pool.QueryRow(context.Background(),
			`SELECT EXISTS(
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
			)`, schema, "users", col,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check column %q: %v", col, err)
		}
		if !exists {
			t.Errorf("system column %q not found in %s.users", col, schema)
		}
	}
}

func TestIdempotent(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_prov")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "items",
						Columns: []config.ColumnConfig{
							{Name: "label", Type: "text"},
						},
					},
				},
			},
		},
	}

	r1, err := prov.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Apply #1: %v", err)
	}
	if len(r1.SchemasCreated) != 1 {
		t.Errorf("Apply #1: expected 1 schema created, got %d", len(r1.SchemasCreated))
	}
	if len(r1.TablesCreated) != 1 {
		t.Errorf("Apply #1: expected 1 table created, got %d", len(r1.TablesCreated))
	}

	r2, err := prov.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Apply #2: %v", err)
	}
	if len(r2.SchemasCreated) != 0 {
		t.Errorf("Apply #2 (idempotent): expected 0 schemas created, got %v", r2.SchemasCreated)
	}
	if len(r2.TablesCreated) != 0 {
		t.Errorf("Apply #2 (idempotent): expected 0 tables created, got %v", r2.TablesCreated)
	}
	if len(r2.ColumnsAdded) != 0 {
		t.Errorf("Apply #2 (idempotent): expected 0 columns added, got %v", r2.ColumnsAdded)
	}
}

func TestAddColumn(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_prov")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)

	cfgV1 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name:    "products",
						Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
					},
				},
			},
		},
	}
	if _, err := prov.Apply(context.Background(), cfgV1); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}

	cfgV2 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "products",
						Columns: []config.ColumnConfig{
							{Name: "title", Type: "text"},
							{Name: "price", Type: "decimal", Required: true},
						},
					},
				},
			},
		},
	}
	r2, err := prov.Apply(context.Background(), cfgV2)
	if err != nil {
		t.Fatalf("Apply v2: %v", err)
	}

	expectedCol := fmt.Sprintf("%s.products.price", schema)
	if len(r2.ColumnsAdded) != 1 || r2.ColumnsAdded[0] != expectedCol {
		t.Errorf("expected ColumnsAdded=[%q], got %v", expectedCol, r2.ColumnsAdded)
	}

	// Confirma via information_schema.
	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
		)`, schema, "products", "price",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check column price: %v", err)
	}
	if !exists {
		t.Errorf("column %q.products.price not found after Apply v2", schema)
	}
}

func TestRenameColumn(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_rename")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)

	cfgV1 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name:    "items",
						Columns: []config.ColumnConfig{{Name: "label", Type: "text"}},
					},
				},
			},
		},
	}
	if _, err := prov.Apply(context.Background(), cfgV1); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}

	cfgV2 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "items",
						Columns: []config.ColumnConfig{
							{Name: "title", Type: "text", RenameFrom: "label"},
						},
					},
				},
			},
		},
	}
	r2, err := prov.Apply(context.Background(), cfgV2)
	if err != nil {
		t.Fatalf("Apply v2: %v", err)
	}

	if len(r2.ColumnsChanged) != 1 {
		t.Fatalf("expected 1 column changed (rename), got %d: %v", len(r2.ColumnsChanged), r2.ColumnsChanged)
	}
	if !strings.Contains(r2.ColumnsChanged[0], "renamed from label") {
		t.Errorf("change description should mention rename: %q", r2.ColumnsChanged[0])
	}
	if len(r2.ColumnsAdded) != 0 {
		t.Errorf("expected 0 columns added, got %d: %v", len(r2.ColumnsAdded), r2.ColumnsAdded)
	}

	// Confirms "label" no longer exists and "title" exists.
	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
		schema, "items", "label",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check label: %v", err)
	}
	if exists {
		t.Errorf("old column 'label' should not exist after rename")
	}

	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
		schema, "items", "title",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check title: %v", err)
	}
	if !exists {
		t.Errorf("new column 'title' should exist after rename")
	}

	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM `+schema+`."_schema_migrations" WHERE description LIKE '%rename%')`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check migration record: %v", err)
	}
	if !exists {
		t.Errorf("migration record should exist for the rename")
	}
}

func TestTypeChange(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_typechg")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)

	cfgV1 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "items",
						Columns: []config.ColumnConfig{
							{Name: "qty", Type: "integer"},
						},
					},
				},
			},
		},
	}
	if _, err := prov.Apply(context.Background(), cfgV1); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}

	cfgV2 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "items",
						Columns: []config.ColumnConfig{
							{Name: "qty", Type: "bigint"},
						},
					},
				},
			},
		},
	}
	r2, err := prov.Apply(context.Background(), cfgV2)
	if err != nil {
		t.Fatalf("Apply v2: %v", err)
	}

	if len(r2.ColumnsChanged) != 1 {
		t.Fatalf("expected 1 column changed (type), got %d: %v", len(r2.ColumnsChanged), r2.ColumnsChanged)
	}
	if !strings.Contains(r2.ColumnsChanged[0], "int4 → int8") {
		t.Errorf("change description should mention type change: %q", r2.ColumnsChanged[0])
	}

	// Confirms the column is now BIGINT (int8).
	var udtName string
	err = pool.QueryRow(context.Background(),
		`SELECT udt_name FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
		schema, "items", "qty",
	).Scan(&udtName)
	if err != nil {
		t.Fatalf("check type: %v", err)
	}
	if udtName != "int8" {
		t.Errorf("expected int8 (bigint), got %s", udtName)
	}

	// Confirma migration record.
	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM `+schema+`."_schema_migrations" WHERE description LIKE '%alter type%')`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check migration record: %v", err)
	}
	if !exists {
		t.Errorf("migration record should exist for the type change")
	}
}

func TestIdempotentRename(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_idemrename")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)

	cfgV1 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name:    "t",
						Columns: []config.ColumnConfig{{Name: "a", Type: "text"}},
					},
				},
			},
		},
	}
	if _, err := prov.Apply(context.Background(), cfgV1); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}

	cfgV2 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name:    "t",
						Columns: []config.ColumnConfig{{Name: "b", Type: "text", RenameFrom: "a"}},
					},
				},
			},
		},
	}
	r2, err := prov.Apply(context.Background(), cfgV2)
	if err != nil {
		t.Fatalf("Apply v2: %v", err)
	}
	if len(r2.ColumnsChanged) != 1 {
		t.Fatalf("v2: expected 1 change, got %d", len(r2.ColumnsChanged))
	}

	r3, err := prov.Apply(context.Background(), cfgV2)
	if err != nil {
		t.Fatalf("Apply v3: %v", err)
	}
	if len(r3.ColumnsChanged) != 0 {
		t.Errorf("v3 (idempotent): expected 0 changes, got %d: %v", len(r3.ColumnsChanged), r3.ColumnsChanged)
	}
	if len(r3.ColumnsAdded) != 0 {
		t.Errorf("v3 (idempotent): expected 0 columns added, got %d", len(r3.ColumnsAdded))
	}
}

func TestRejectUnsafeTypeChange(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_unsafe")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)

	cfgV1 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "t",
						Columns: []config.ColumnConfig{
							{Name: "active", Type: "boolean"},
						},
					},
				},
			},
		},
	}
	if _, err := prov.Apply(context.Background(), cfgV1); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}

	cfgV2 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "t",
						Columns: []config.ColumnConfig{
							{Name: "active", Type: "integer"},
						},
					},
				},
			},
		},
	}
	_, err := prov.Apply(context.Background(), cfgV2)
	if err == nil {
		t.Fatal("expected error for unsafe type change (bool→int), got nil")
	}
	if !strings.Contains(err.Error(), "unsafe conversion") {
		t.Errorf("error should mention 'unsafe conversion': %v", err)
	}
}

func TestRenameThenAddColumn(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_rename_add")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)

	cfgV1 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name:    "t",
						Columns: []config.ColumnConfig{{Name: "oldname", Type: "text"}},
					},
				},
			},
		},
	}
	if _, err := prov.Apply(context.Background(), cfgV1); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}

	cfgV2 := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name: "t",
						Columns: []config.ColumnConfig{
							{Name: "newname", Type: "text", RenameFrom: "oldname"},
							{Name: "extra", Type: "integer"},
						},
					},
				},
			},
		},
	}
	r2, err := prov.Apply(context.Background(), cfgV2)
	if err != nil {
		t.Fatalf("Apply v2: %v", err)
	}

	if len(r2.ColumnsChanged) != 1 {
		t.Errorf("expected 1 change (rename), got %d: %v", len(r2.ColumnsChanged), r2.ColumnsChanged)
	}
	if len(r2.ColumnsAdded) != 1 {
		t.Errorf("expected 1 column added, got %d: %v", len(r2.ColumnsAdded), r2.ColumnsAdded)
	}

	// Confirma "newname" e "extra" existem.
	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
		schema, "t", "newname",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check newname: %v", err)
	}
	if !exists {
		t.Errorf("'newname' should exist after rename")
	}

	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
		schema, "t", "extra",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check extra: %v", err)
	}
	if !exists {
		t.Errorf("'extra' should exist after add")
	}

	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
		schema, "t", "oldname",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check oldname: %v", err)
	}
	if exists {
		t.Errorf("'oldname' should not exist after rename")
	}
}
