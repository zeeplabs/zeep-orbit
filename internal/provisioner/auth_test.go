package provisioner_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestAuthUserRoleColumn_Idempotent verifies ROWPOL-01/03/04: the migration
// adding `role` to `_auth_users` can run twice without error, existing rows
// default to 'member', and the column accepts arbitrary string values (no
// CHECK/enum constraint).
func TestAuthUserRoleColumn_Idempotent(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	schema := uniqueSchema("test_role_col")
	t.Cleanup(func() { dropSchema(t, pool, schema) })

	prov := provisioner.New(pool)
	cfg := &config.Config{
		Apps: []config.AppConfig{
			{
				Name: schema,
				Auth: config.AuthConfig{
					Providers: config.AuthProviders{Email: true},
				},
			},
		},
	}

	if _, err := prov.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// Insert a row the way a pre-existing user (created before this feature)
	// would look: no explicit role supplied.
	insertSQL := fmt.Sprintf(`INSERT INTO %q."_auth_users" ("email", "password_hash") VALUES ('existing@example.com', 'x')`, schema)
	if _, err := pool.Exec(context.Background(), insertSQL); err != nil {
		t.Fatalf("insert existing-style user: %v", err)
	}

	// Running the migration again must not error (idempotency).
	if err := prov.EnsureAuthUserColumns(context.Background(), schema); err != nil {
		t.Fatalf("second EnsureAuthUserColumns (idempotency): %v", err)
	}

	selectSQL := fmt.Sprintf(`SELECT "role" FROM %q."_auth_users" WHERE "email" = 'existing@example.com'`, schema)
	var role string
	if err := pool.QueryRow(context.Background(), selectSQL).Scan(&role); err != nil {
		t.Fatalf("select role: %v", err)
	}
	if role != "member" {
		t.Fatalf("expected default role 'member' for pre-existing row, got %q", role)
	}

	// Column accepts any free-form string value (no CHECK/enum constraint).
	updateSQL := fmt.Sprintf(`UPDATE %q."_auth_users" SET "role" = 'approver' WHERE "email" = 'existing@example.com'`, schema)
	if _, err := pool.Exec(context.Background(), updateSQL); err != nil {
		t.Fatalf("update role to custom value: %v", err)
	}

	if err := pool.QueryRow(context.Background(), selectSQL).Scan(&role); err != nil {
		t.Fatalf("select updated role: %v", err)
	}
	if role != "approver" {
		t.Fatalf("expected custom role 'approver' to be accepted, got %q", role)
	}
}
