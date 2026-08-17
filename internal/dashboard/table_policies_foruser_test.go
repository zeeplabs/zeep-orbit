package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestCreateTablePolicyForUser_HappyPath covers T7's Done-when: calling
// CreateTablePolicyForUser directly (no HTTP layer) creates a policy and
// produces the pre-extraction audit_log row shape (mcp-server spec
// MCP-13/MCP-10).
func TestCreateTablePolicyForUser_HappyPath(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	row, err := h.CreateTablePolicyForUser(ctx, actors["appadmin"], appID, table, PolicyDef{
		Name:    "select_active_direct",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "status", Operator: "=", ValueSource: "literal", Value: "active"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateTablePolicyForUser: %v", err)
	}
	if row.PgPolicyName != "select_active_direct" {
		t.Fatalf("expected pg_policy_name %q, got %q", "select_active_direct", row.PgPolicyName)
	}

	var action, resourceType string
	if err := pool.QueryRow(ctx,
		`SELECT action, resource_type FROM zeep_system.audit_log WHERE resource_type = 'table_policy' AND resource_id = $1`,
		row.ID,
	).Scan(&action, &resourceType); err != nil {
		t.Fatalf("expected an audit_log row for the created policy: %v", err)
	}
	if action != "app.table_policy.create" {
		t.Fatalf("expected audit action %q, got %q", "app.table_policy.create", action)
	}
}

// TestCreateTablePolicyForUser_InvalidClauseReturnsValidationError covers
// T7's Done-when validation-failure case: a clause referencing an unknown
// column (provisioner.ValidationError, same as the HTTP-level test
// TestCreateTablePolicyHandler_InvalidClauseReturns400WithBuilderMessage).
func TestCreateTablePolicyForUser_InvalidClauseReturnsValidationError(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.CreateTablePolicyForUser(ctx, actors["appadmin"], appID, table, PolicyDef{
		Name:    "bad_direct",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "does_not_exist", Operator: "=", ValueSource: "literal", Value: "x"}},
	}, "127.0.0.1")
	var valErr *provisioner.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *provisioner.ValidationError, got %v (%T)", err, err)
	}
}

// TestCreateTablePolicyForUser_DuplicateReturnsErrPolicyAlreadyExists covers
// T7's Done-when validation-failure case: a duplicate policy name (same as
// the HTTP-level test TestCreateTablePolicyHandler_DuplicateReturns409).
func TestCreateTablePolicyForUser_DuplicateReturnsErrPolicyAlreadyExists(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	def := PolicyDef{
		Name:    "dup_direct",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "status", Operator: "IS NOT NULL"}},
	}
	if _, err := h.CreateTablePolicyForUser(ctx, actors["appadmin"], appID, table, def, "127.0.0.1"); err != nil {
		t.Fatalf("first CreateTablePolicyForUser: %v", err)
	}
	_, err := h.CreateTablePolicyForUser(ctx, actors["appadmin"], appID, table, def, "127.0.0.1")
	if !errors.Is(err, ErrPolicyAlreadyExists) {
		t.Fatalf("expected ErrPolicyAlreadyExists, got %v", err)
	}
}

// TestCreateTablePolicyForUser_NonManagerForbidden covers the authorization
// half of the extraction (same as the HTTP-level test
// TestCreateTablePolicyHandler_NonAdminForbidden's editor case): CanManage
// is required, editor is rejected with ErrForbidden.
func TestCreateTablePolicyForUser_NonManagerForbidden(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.CreateTablePolicyForUser(ctx, actors["appeditor"], appID, table, PolicyDef{
		Name:    "editor_attempt",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "status", Operator: "IS NOT NULL"}},
	}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

// TestCreateTablePolicyForUser_UnknownTableReturnsErrTableNotFound covers
// the by-name table lookup this function performs.
func TestCreateTablePolicyForUser_UnknownTableReturnsErrTableNotFound(t *testing.T) {
	pool, h, actors, appID, _ := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.CreateTablePolicyForUser(ctx, actors["appadmin"], appID, "no-such-table", PolicyDef{
		Name:    "irrelevant",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "status", Operator: "IS NOT NULL"}},
	}, "127.0.0.1")
	if !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("expected ErrTableNotFound, got %v", err)
	}
}

// TestListTablePoliciesForUser_HappyPathMatchesRESTShape covers
// mcp-read-only-tools T4's Done-when: ListTablePoliciesForUser returns the
// same policies ListTablePolicies' existing REST test
// (TestListTablePoliciesHandler) expects for the same fixture.
func TestListTablePoliciesForUser_HappyPathMatchesRESTShape(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	if _, err := h.CreateTablePolicyForUser(ctx, actors["appadmin"], appID, table, PolicyDef{
		Name:    "list_for_user",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "status", Operator: "IS NOT NULL"}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateTablePolicyForUser: %v", err)
	}

	rows, err := ListTablePoliciesForUser(ctx, pool, actors["appadmin"], appID, table)
	if err != nil {
		t.Fatalf("ListTablePoliciesForUser: %v", err)
	}
	if len(rows) != 1 || rows[0].PgPolicyName != "list_for_user" {
		t.Fatalf("expected 1 policy named %q, got %+v", "list_for_user", rows)
	}
}

// TestListTablePoliciesForUser_NonManagerForbidden covers mcp-read-only-tools
// T4's Done-when: an editor (a real app member whose role fails
// CanManage(), not just "no membership") is rejected with ErrForbidden —
// the explicit CanManage() tier test the design's Authorization Matrix
// requires, not a reused "same as GetApp" assertion.
func TestListTablePoliciesForUser_NonManagerForbidden(t *testing.T) {
	pool, _, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListTablePoliciesForUser(ctx, pool, actors["appeditor"], appID, table)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanManage()==false), got %v", err)
	}
}

// TestListTablePoliciesForUser_UnknownAppReturnsErrNotFound covers
// mcp-read-only-tools T4's Done-when: an outsider with no membership at all
// (and not superadmin/CanReadAnyApp) gets the same not-found GetApp already
// returns.
func TestListTablePoliciesForUser_UnknownAppReturnsErrNotFound(t *testing.T) {
	pool, _, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListTablePoliciesForUser(ctx, pool, actors["outsider"], appID, table)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an outsider with no app visibility, got %v", err)
	}
}

// TestListTablePoliciesForUser_UnknownTableReturnsErrTableNotFound covers
// mcp-read-only-tools T4's Done-when: a valid app + nonexistent table name
// returns ErrTableNotFound, never an empty list (spec AC4: an empty list
// means "table exists, zero policies").
func TestListTablePoliciesForUser_UnknownTableReturnsErrTableNotFound(t *testing.T) {
	pool, _, actors, appID, _ := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListTablePoliciesForUser(ctx, pool, actors["appadmin"], appID, "no-such-table")
	if !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("expected ErrTableNotFound, got %v", err)
	}
}

// TestListTablePoliciesForUser_EmptyReturnsEmptyArrayNeverNil covers
// mcp-read-only-tools T4's Done-when (spec "Empty-result shape"
// assumption): a valid app+table with zero policies returns an empty
// slice, not nil.
func TestListTablePoliciesForUser_EmptyReturnsEmptyArrayNeverNil(t *testing.T) {
	pool, _, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	rows, err := ListTablePoliciesForUser(ctx, pool, actors["appadmin"], appID, table)
	if err != nil {
		t.Fatalf("ListTablePoliciesForUser: %v", err)
	}
	if rows == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero policies on a fresh table, got %+v", rows)
	}
}
