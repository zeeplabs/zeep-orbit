package provisioner

import (
	"errors"
	"strings"
	"testing"
)

func TestTypeChangeError_DoesNotLeakCause(t *testing.T) {
	cause := errors.New("ERROR: SQLSTATE 22P02 invalid input syntax")
	e := &TypeChangeError{
		Column:      "amount",
		CurrentType: "text",
		DesiredType: "int4",
		Reason:      ReasonRuntimeFailure,
		Cause:       cause,
	}

	msg := e.Error()
	if strings.Contains(msg, "SQLSTATE") || strings.Contains(msg, cause.Error()) {
		t.Fatalf("public error message leaks internal cause: %q", msg)
	}

	if !errors.Is(errors.Unwrap(e), cause) {
		t.Fatalf("Unwrap() did not return the original cause")
	}
}

func TestForeignKeyViolationError_DoesNotLeakCause(t *testing.T) {
	cause := errors.New("ERROR: SQLSTATE 23503 insert or update on table \"orders\" violates foreign key constraint")
	e := &ForeignKeyViolationError{
		Column: "customer_id",
		Detail: "Key (customer_id)=(deadbeef) is not present in table \"customers\".",
		Cause:  cause,
	}

	msg := e.Error()
	if strings.Contains(msg, "SQLSTATE") || strings.Contains(msg, cause.Error()) {
		t.Fatalf("public error message leaks internal cause: %q", msg)
	}

	if !errors.Is(errors.Unwrap(e), cause) {
		t.Fatalf("Unwrap() did not return the original cause")
	}
}

func TestForeignKeyViolationError_MessageIncludesColumnAndDetail(t *testing.T) {
	e := &ForeignKeyViolationError{
		Column: "customer_id",
		Detail: "Key (customer_id)=(deadbeef) is not present in table \"customers\".",
	}

	msg := e.Error()
	if !strings.Contains(msg, "customer_id") {
		t.Errorf("expected message to contain column name, got: %q", msg)
	}
	if !strings.Contains(msg, "Key (customer_id)=(deadbeef) is not present in table \"customers\".") {
		t.Errorf("expected message to contain the detail text, got: %q", msg)
	}
}

// CENUM-10: the rejection error names the offending value and its exact
// row count.
func TestEnumValueInUseError_MessageNamesSingleValueAndCount(t *testing.T) {
	e := &EnumValueInUseError{Column: "status", Counts: map[string]int{"closed": 1}}

	want := `cannot remove value(s) from "status": "closed" is used by 1 row(s)`
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// CENUM-10: every offending value is named with its own count, not just the
// first one found. The message is deterministic despite Counts being a map.
func TestEnumValueInUseError_MessageNamesEveryOffendingValue(t *testing.T) {
	e := &EnumValueInUseError{
		Column: "status",
		Counts: map[string]int{"closed": 3, "archived": 12},
	}

	want := `cannot remove value(s) from "status": "archived" is used by 12 row(s), "closed" is used by 3 row(s)`
	for i := 0; i < 5; i++ {
		if got := e.Error(); got != want {
			t.Fatalf("Error() = %q, want %q", got, want)
		}
	}
}

func TestEnumValueInUseError_DoesNotLeakCause(t *testing.T) {
	cause := errors.New(`ERROR: SQLSTATE 23514 new row for relation "assets" violates check constraint "assets_status_check"`)
	e := &EnumValueInUseError{
		Column: "status",
		Counts: map[string]int{"closed": 1},
		Cause:  cause,
	}

	msg := e.Error()
	if strings.Contains(msg, "SQLSTATE") || strings.Contains(msg, cause.Error()) {
		t.Fatalf("public error message leaks internal cause: %q", msg)
	}

	if !errors.Is(errors.Unwrap(e), cause) {
		t.Fatalf("Unwrap() did not return the original cause")
	}
}

func TestTypeChangeError_ReasonsProduceDistinctMessages(t *testing.T) {
	base := &TypeChangeError{Column: "c", CurrentType: "text", DesiredType: "int4"}

	cases := []struct {
		reason Reason
		want   string
	}{
		{ReasonNoConversionsDefined, "does not support automatic conversion"},
		{ReasonUnsafeNarrowing, "would narrow or lose data"},
		{ReasonRuntimeFailure, "check that existing data is compatible"},
	}
	for _, tc := range cases {
		base.Reason = tc.reason
		if !strings.Contains(base.Error(), tc.want) {
			t.Errorf("reason %q: expected message to contain %q, got %q", tc.reason, tc.want, base.Error())
		}
	}
}
