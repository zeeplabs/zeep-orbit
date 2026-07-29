package provisioner

import "fmt"

// Reason categorizes why a column type change was rejected.
type Reason string

const (
	ReasonNoConversionsDefined Reason = "no_conversions_defined"
	ReasonUnsafeNarrowing      Reason = "unsafe_narrowing"
	ReasonRuntimeFailure       Reason = "runtime_failure"
)

// TypeChangeError represents a rejected or failed column type change.
// Error() is safe to expose to end users; Cause carries the original
// error (e.g. from pgx) for server-side logging only, reachable via Unwrap.
type TypeChangeError struct {
	Column      string
	CurrentType string
	DesiredType string
	Reason      Reason
	Cause       error
}

func (e *TypeChangeError) Error() string {
	return fmt.Sprintf("cannot change type of %q from %s to %s: %s", e.Column, e.CurrentType, e.DesiredType, e.publicReason())
}

func (e *TypeChangeError) Unwrap() error { return e.Cause }

func (e *TypeChangeError) publicReason() string {
	switch e.Reason {
	case ReasonNoConversionsDefined:
		return fmt.Sprintf("source type %s does not support automatic conversion", e.CurrentType)
	case ReasonUnsafeNarrowing:
		return "unsafe conversion — would narrow or lose data"
	case ReasonRuntimeFailure:
		return "conversion failed — check that existing data is compatible with the new type"
	default:
		return "conversion failed"
	}
}
