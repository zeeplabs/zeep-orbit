package provisioner_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

func TestNumericColumnTypeCreatesDecimal(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_numeric")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Tables: []config.TableConfig{
					{
						Name:    "invoices",
						Columns: []config.ColumnConfig{{Name: "amount", Type: "numeric"}},
					},
				},
			},
		},
	}

	if _, err := prov.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var udtName string
	if err := pool.QueryRow(context.Background(),
		`SELECT udt_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'invoices' AND column_name = 'amount'`,
		schema,
	).Scan(&udtName); err != nil {
		t.Fatalf("query column type: %v", err)
	}
	if udtName != "numeric" {
		t.Errorf("expected column type 'numeric' (from DECIMAL), got %q — 'numeric' input silently became TEXT", udtName)
	}
}

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

// TestForeignKeyToAuthUsersEnforced proves ROWPOL-21/22/24: a business
// column can declare an explicit FK to "_auth_users"."id" (not just the
// automatic, implicit owner_id FK) and Postgres enforces it for real —
// inserting a requester_id that doesn't exist in _auth_users fails with a
// genuine FK violation, not a soft application-level check.
func TestForeignKeyToAuthUsersEnforced(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_fk_authusers")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Auth: config.AuthConfig{Providers: config.AuthProviders{Email: true}},
				Tables: []config.TableConfig{
					{
						Name: "requests",
						Columns: []config.ColumnConfig{
							{
								Name: "requester_id",
								Type: "uuid",
								References: &config.ReferenceConfig{
									Table: "_auth_users", Column: "id", OnDelete: "cascade",
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := prov.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	ctx := context.Background()

	var fkExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM information_schema.table_constraints tc
			JOIN information_schema.constraint_column_usage ccu
				ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
			WHERE tc.table_schema = $1 AND tc.table_name = 'requests'
				AND tc.constraint_type = 'FOREIGN KEY' AND ccu.table_name = '_auth_users'
		)`, schema,
	).Scan(&fkExists); err != nil {
		t.Fatalf("check FK constraint exists: %v", err)
	}
	if !fkExists {
		t.Fatal("expected a real FOREIGN KEY constraint from requests.requester_id to _auth_users, none found")
	}

	var authUserID string
	if err := pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %q."_auth_users" (email, password_hash) VALUES ('rowpol-t17@example.com', 'x') RETURNING id`, schema),
	).Scan(&authUserID); err != nil {
		t.Fatalf("insert _auth_users row: %v", err)
	}

	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q.requests (requester_id) VALUES ($1)`, schema), authUserID,
	); err != nil {
		t.Fatalf("insert requests row with a real requester_id: %v", err)
	}

	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q.requests (requester_id) VALUES (gen_random_uuid())`, schema),
	); err == nil {
		t.Fatal("expected a foreign key violation inserting a requester_id absent from _auth_users, got nil")
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
