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
