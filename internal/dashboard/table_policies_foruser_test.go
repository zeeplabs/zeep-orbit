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
