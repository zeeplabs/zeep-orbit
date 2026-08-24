package dashboard

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func tablePoliciesTestPool(t *testing.T) *db.Pool {
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
	t.Cleanup(pool.Close)

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS tp_test CASCADE`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.table_policies, zeep_system.app_tables, zeep_system.apps, zeep_system.dashboard_users CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx,
		`CREATE SCHEMA tp_test`,
	); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`CREATE TABLE tp_test.requests (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			requester_id UUID NOT NULL,
			status       TEXT NOT NULL DEFAULT 'pending'
		)`,
	); err != nil {
		t.Fatalf("create test table: %v", err)
	}

	return pool
}

func tablePoliciesTestApp(t *testing.T, pool *db.Pool) (appID, userID string) {
	t.Helper()
	userID = testUser(t, pool, "policy-admin@example.com", "superadmin")
	err := pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"policy-test-app", userID,
	).Scan(&appID)
	if err != nil {
		t.Fatalf("create test app: %v", err)
	}
	return appID, userID
}

func requestsColumns() []config.ColumnConfig {
	return []config.ColumnConfig{
		{Name: "requester_id", Type: "uuid"},
		{Name: "status", Type: "text"},
	}
}

func relRowSecurityEnabled(t *testing.T, pool *db.Pool, schema, table string) bool {
	t.Helper()
	var enabled bool
	err := pool.QueryRow(context.Background(),
		`SELECT relrowsecurity FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1 AND c.relname = $2`,
		schema, table,
	).Scan(&enabled)
	if err != nil {
		t.Fatalf("check relrowsecurity: %v", err)
	}
	return enabled
}

func TestCreateTablePolicy_EnablesRowLevelSecurityOnlyOnce(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	if relRowSecurityEnabled(t, pool, "tp_test", "requests") {
		t.Fatal("expected RLS disabled before any policy exists")
	}

	def1 := PolicyDef{
		Name:   "approver_no_self_approve",
		Action: "update",
		Roles:  []string{"approver"},
		Clauses: []PolicyClause{
			{Column: "requester_id", Operator: "!=", ValueSource: "claim", Value: "sub"},
		},
	}
	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def1, userID); err != nil {
		t.Fatalf("CreateTablePolicy (1st): %v", err)
	}
	if !relRowSecurityEnabled(t, pool, "tp_test", "requests") {
		t.Fatal("expected RLS enabled after first policy")
	}

	def2 := PolicyDef{
		Name:   "select_active_only",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "active"},
		},
	}
	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def2, userID); err != nil {
		t.Fatalf("CreateTablePolicy (2nd): %v", err)
	}
	if !relRowSecurityEnabled(t, pool, "tp_test", "requests") {
		t.Fatal("expected RLS to remain enabled after second policy")
	}

	policies, err := ListTablePolicies(ctx, pool, appID, "requests")
	if err != nil {
		t.Fatalf("ListTablePolicies: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}
}

func TestCreateTablePolicy_InvalidClauseRejectedNoDDL(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	def := PolicyDef{
		Name:   "bad_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "does_not_exist", Operator: "=", ValueSource: "literal", Value: "x"},
		},
	}
	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def, userID); err == nil {
		t.Fatal("expected error for invalid clause, got nil")
	}
	if relRowSecurityEnabled(t, pool, "tp_test", "requests") {
		t.Fatal("RLS must not be enabled when policy creation failed validation")
	}
	policies, err := ListTablePolicies(ctx, pool, appID, "requests")
	if err != nil {
		t.Fatalf("ListTablePolicies: %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("expected no persisted policy row, got %d", len(policies))
	}
}

func TestCreateTablePolicy_DuplicateNameReturnsErrPolicyAlreadyExists(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	def := PolicyDef{
		Name:   "dup_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}
	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def, userID); err != nil {
		t.Fatalf("CreateTablePolicy (1st): %v", err)
	}

	def2 := def
	def2.Roles = []string{"approver"}
	_, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def2, userID)
	if err == nil {
		t.Fatal("expected error for duplicate policy name/action/table, got nil")
	}
	if err != ErrPolicyAlreadyExists {
		t.Fatalf("expected ErrPolicyAlreadyExists, got %v", err)
	}
}

func TestDeleteTablePolicy_LastPolicyLeavesRLSEnabled(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	def := PolicyDef{
		Name:   "only_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}
	row, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def, userID)
	if err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}

	if err := DeleteTablePolicy(ctx, pool, appID, "tp_test", row.ID); err != nil {
		t.Fatalf("DeleteTablePolicy: %v", err)
	}

	if !relRowSecurityEnabled(t, pool, "tp_test", "requests") {
		t.Fatal("expected RLS to remain enabled (default-deny) after deleting the last policy")
	}

	policies, err := ListTablePolicies(ctx, pool, appID, "requests")
	if err != nil {
		t.Fatalf("ListTablePolicies: %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("expected 0 policies after delete, got %d", len(policies))
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_policies WHERE schemaname = 'tp_test' AND tablename = 'requests'`,
	).Scan(&count); err != nil {
		t.Fatalf("count pg_policies: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected native policy dropped, pg_policies still has %d rows", count)
	}
}

func TestDeleteTablePolicy_NotFound(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, _ := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	err := DeleteTablePolicy(ctx, pool, appID, "tp_test", "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// pgPolicyQual returns the native policy's USING expression (qual). App
// roles are enforced inside this expression via current_setting(...) = ANY
// (ARRAY[...]) — the native pg_policies.roles column is always the single
// shared zeep_app_enduser Postgres role (provisioner.BuildPolicySQL), so
// asserting on the app-level role names means checking qual, not roles.
func pgPolicyQual(t *testing.T, pool *db.Pool, schema, table, policyName string) string {
	t.Helper()
	var qual string
	err := pool.QueryRow(context.Background(),
		`SELECT qual FROM pg_policies WHERE schemaname = $1 AND tablename = $2 AND policyname = $3`,
		schema, table, policyName,
	).Scan(&qual)
	if err != nil {
		t.Fatalf("query pg_policies qual for %q: %v", policyName, err)
	}
	return qual
}

func pgPolicyExists(t *testing.T, pool *db.Pool, schema, table, policyName string) bool {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pg_policies WHERE schemaname = $1 AND tablename = $2 AND policyname = $3`,
		schema, table, policyName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count pg_policies for %q: %v", policyName, err)
	}
	return count > 0
}

func TestUpdateTablePolicy_HappyPathReflectsNewRolesAndTimestamps(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	created, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), PolicyDef{
		Name:   "editable_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID)
	if err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}
	if created.UpdatedAt != nil {
		t.Fatalf("expected nil UpdatedAt on creation, got %v", created.UpdatedAt)
	}

	updated, err := UpdateTablePolicy(ctx, pool, appID, "tp_test", "requests", created.ID, requestsColumns(), PolicyDef{
		Name:   "editable_policy",
		Action: "select",
		Roles:  []string{"member", "admin"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID)
	if err != nil {
		t.Fatalf("UpdateTablePolicy: %v", err)
	}

	if len(updated.Roles) != 2 || updated.Roles[0] != "member" || updated.Roles[1] != "admin" {
		t.Fatalf("expected catalog roles [member admin], got %v", updated.Roles)
	}
	if updated.UpdatedAt == nil {
		t.Fatal("expected UpdatedAt to be set after edit")
	}
	if updated.UpdatedBy == nil || *updated.UpdatedBy != userID {
		t.Fatalf("expected UpdatedBy=%q, got %v", userID, updated.UpdatedBy)
	}

	qual := pgPolicyQual(t, pool, "tp_test", "requests", "editable_policy")
	if !strings.Contains(qual, "'admin'") {
		t.Fatalf("expected pg_policies USING clause to include the new 'admin' role, got %q", qual)
	}
}

func TestUpdateTablePolicy_NotFoundReturnsErrPolicyNotFound(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	def := PolicyDef{
		Name:   "unrelated_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}

	_, err := UpdateTablePolicy(ctx, pool, appID, "tp_test", "requests", "00000000-0000-0000-0000-000000000000", requestsColumns(), def, userID)
	if err != ErrPolicyNotFound {
		t.Fatalf("expected ErrPolicyNotFound for missing policyID, got %v", err)
	}

	var otherAppID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"policy-test-app-2", userID,
	).Scan(&otherAppID); err != nil {
		t.Fatalf("create second test app: %v", err)
	}
	created, err := CreateTablePolicy(ctx, pool, otherAppID, "tp_test", "requests", requestsColumns(), def, userID)
	if err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}
	if _, err := UpdateTablePolicy(ctx, pool, appID, "tp_test", "requests", created.ID, requestsColumns(), def, userID); err != ErrPolicyNotFound {
		t.Fatalf("expected ErrPolicyNotFound for policy owned by another app, got %v", err)
	}
}

func TestUpdateTablePolicy_InvalidClauseRejectedNoMutation(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	created, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), PolicyDef{
		Name:   "stays_intact",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID)
	if err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}

	badDef := PolicyDef{
		Name:   "stays_intact",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "does_not_exist", Operator: "=", ValueSource: "literal", Value: "x"},
		},
	}
	if _, err := UpdateTablePolicy(ctx, pool, appID, "tp_test", "requests", created.ID, requestsColumns(), badDef, userID); err == nil {
		t.Fatal("expected error for invalid clause, got nil")
	}

	qual := pgPolicyQual(t, pool, "tp_test", "requests", "stays_intact")
	if !strings.Contains(qual, "'member'") || strings.Contains(qual, "'admin'") {
		t.Fatalf("expected native policy unchanged after rejected edit, got qual %q", qual)
	}
	policies, err := ListTablePolicies(ctx, pool, appID, "requests")
	if err != nil {
		t.Fatalf("ListTablePolicies: %v", err)
	}
	if len(policies) != 1 || policies[0].UpdatedAt != nil {
		t.Fatalf("expected catalog row unchanged (no updated_at) after rejected edit, got %+v", policies)
	}
}

func TestUpdateTablePolicy_ConflictReturnsErrPolicyConflict(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), PolicyDef{
		Name:   "taken_name",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID); err != nil {
		t.Fatalf("CreateTablePolicy (taken_name): %v", err)
	}

	toEdit, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), PolicyDef{
		Name:   "movable_name",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID)
	if err != nil {
		t.Fatalf("CreateTablePolicy (movable_name): %v", err)
	}

	_, err = UpdateTablePolicy(ctx, pool, appID, "tp_test", "requests", toEdit.ID, requestsColumns(), PolicyDef{
		Name:   "taken_name",
		Action: "select",
		Roles:  []string{"admin"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID)
	if err != ErrPolicyConflict {
		t.Fatalf("expected ErrPolicyConflict, got %v", err)
	}

	qual := pgPolicyQual(t, pool, "tp_test", "requests", "movable_name")
	if !strings.Contains(qual, "'member'") || strings.Contains(qual, "'admin'") {
		t.Fatalf("expected movable_name policy unchanged after rejected conflicting edit, got qual %q", qual)
	}
}

func TestUpdateTablePolicy_RenameDropsOldNativePolicy(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	created, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), PolicyDef{
		Name:   "old_name",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID)
	if err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}

	if _, err := UpdateTablePolicy(ctx, pool, appID, "tp_test", "requests", created.ID, requestsColumns(), PolicyDef{
		Name:   "new_name",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}, userID); err != nil {
		t.Fatalf("UpdateTablePolicy (rename): %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_policies WHERE schemaname = 'tp_test' AND tablename = 'requests' AND policyname = 'old_name'`,
	).Scan(&count); err != nil {
		t.Fatalf("count pg_policies for old_name: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected old_name native policy dropped, still has %d rows", count)
	}

	if !pgPolicyExists(t, pool, "tp_test", "requests", "new_name") {
		t.Fatal("expected new_name native policy to be present after rename")
	}
}

// TestDeleteAppTable_CascadesTablePolicies covers the spec edge case: deleting
// a table's metadata row (DeleteAppTable) must also delete its table_policies
// rows, even though there is no DB-level FK from table_policies to
// app_tables (table_policies.app_id only cascades on whole-app deletion).
func TestDeleteAppTable_CascadesTablePolicies(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	tableRow, err := InsertAppTable(ctx, pool, appID, AppTableRow{
		Name:    "requests",
		Columns: requestsColumns(),
	})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	def := PolicyDef{
		Name:   "cascade_check",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}
	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def, userID); err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}

	if _, err := DeleteAppTable(ctx, pool, appID, tableRow.ID); err != nil {
		t.Fatalf("DeleteAppTable: %v", err)
	}

	policies, err := ListTablePolicies(ctx, pool, appID, "requests")
	if err != nil {
		t.Fatalf("ListTablePolicies: %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("expected table_policies rows to be cascaded away, got %d", len(policies))
	}
}
