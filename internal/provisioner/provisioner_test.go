package provisioner_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-core/internal/config"
	"github.com/zeeplabs/zeep-core/internal/db"
	"github.com/zeeplabs/zeep-core/internal/provisioner"
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

// uniqueSchema gera um nome de schema único por teste para evitar colisões.
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

	// Confirma colunas de sistema via information_schema.
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

	// Primeira chamada — cria tudo.
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

	// Segunda chamada — nada deve ser criado ou alterado.
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

	// Primeiro Apply: cria schema e tabela com 1 coluna.
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

	// Segundo Apply: adiciona nova coluna "price".
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
