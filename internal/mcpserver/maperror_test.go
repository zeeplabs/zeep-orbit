package mcpserver

import (
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestMapWriteError_ProvisionerValidationErrorSurfacedVerbatim covers a
// design.md-flagged gap: mapWriteError originally had no case for
// *provisioner.ValidationError (BuildPolicySQL's own input-validation
// failures — unknown column, bad operator, bad claim, etc.), so every such
// failure fell through to the generic internalErr, giving a caller no way to
// tell a bad-input mistake from a platform failure. This asserts the real
// ValidationError message is returned unchanged instead of the generic
// "internal error".
func TestMapWriteError_ProvisionerValidationErrorSurfacedVerbatim(t *testing.T) {
	_, err := provisioner.BuildPolicySQL("app_schema", "notes", provisioner.PolicyDef{
		Name:   "bad_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []provisioner.PolicyClause{
			{Column: "does_not_exist", Operator: "=", ValueSource: "literal", Value: "x"},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected BuildPolicySQL to reject an unknown column, got nil error")
	}

	mapped := mapWriteError(err)

	if mapped.Error() == errInternal.Error() {
		t.Fatalf("expected the real validation message, got the generic internal error %q", mapped.Error())
	}
	if mapped.Error() != err.Error() {
		t.Fatalf("expected mapWriteError to return the ValidationError verbatim, got %q want %q", mapped.Error(), err.Error())
	}
}
