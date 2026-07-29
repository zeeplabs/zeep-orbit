package provisioner_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

func TestForeignKeyCascadeDelete(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_fk")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					// Declared out of dependency order on purpose: orders
					// references customers, but customers comes second here.
					{
						Name: "orders",
						Columns: []config.ColumnConfig{
							{Name: "customer_id", Type: "uuid", Required: true, References: &config.ReferenceConfig{
								Table: "customers", Column: "id", OnDelete: "cascade",
							}},
						},
					},
					{
						Name:    "customers",
						Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
					},
				},
			},
		},
	}

	if _, err := prov.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	ctx := context.Background()

	var customerID string
	if err := pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %q.customers (name) VALUES ('acme') RETURNING id`, schema),
	).Scan(&customerID); err != nil {
		t.Fatalf("insert customer: %v", err)
	}

	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q.orders (customer_id) VALUES ($1)`, schema), customerID,
	); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %q.customers WHERE id = $1`, schema), customerID); err != nil {
		t.Fatalf("delete customer: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %q.orders`, schema)).Scan(&remaining); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if remaining != 0 {
		t.Errorf("expected ON DELETE CASCADE to remove dependent orders, %d remain", remaining)
	}
}

func TestEnsureIndexes(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_idx")
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
							{Name: "email", Type: "text"},
							{Name: "org_id", Type: "uuid"},
						},
						Indexes: []config.IndexConfig{
							{Name: "idx_users_email", Columns: []string{"email"}, Unique: true},
							{Name: "idx_users_org_email", Columns: []string{"org_id", "email"}},
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
	if len(report.IndexesCreated) != 2 {
		t.Fatalf("expected 2 indexes created, got %v", report.IndexesCreated)
	}

	for _, idxName := range []string{"idx_users_email", "idx_users_org_email"} {
		var exists bool
		if err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = $1 AND indexname = $2)`,
			schema, idxName,
		).Scan(&exists); err != nil {
			t.Fatalf("check index %q: %v", idxName, err)
		}
		if !exists {
			t.Errorf("index %q not found", idxName)
		}
	}

	// Re-apply must be idempotent (IF NOT EXISTS) and must not fail.
	if _, err := prov.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("second Apply (idempotency check): %v", err)
	}
}

func TestDropTableRejectsWhenReferenced(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_dropfk")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{Name: "customers", Columns: []config.ColumnConfig{{Name: "name", Type: "text"}}},
					{
						Name: "orders",
						Columns: []config.ColumnConfig{
							{Name: "customer_id", Type: "uuid", References: &config.ReferenceConfig{Table: "customers", Column: "id"}},
						},
					},
				},
			},
		},
	}

	if _, err := prov.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	err := prov.DropTable(context.Background(), schema, "customers")
	if err == nil {
		t.Fatal("expected DropTable to reject a table still referenced by a foreign key")
	}

	var exists bool
	if qErr := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'customers')`,
		schema,
	).Scan(&exists); qErr != nil {
		t.Fatalf("check table exists: %v", qErr)
	}
	if !exists {
		t.Error("customers table was dropped despite being referenced")
	}
}
