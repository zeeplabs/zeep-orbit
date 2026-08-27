package provisioner

import (
	"fmt"
	"sort"
	"strings"
)

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

// ForeignKeyViolationError represents a rejected ADD FOREIGN KEY DDL because
// existing rows violate the new constraint (Postgres error 23503).
// Error() is safe to expose to end users; Cause carries the original
// error (e.g. from pgx) for server-side logging only, reachable via Unwrap.
type ForeignKeyViolationError struct {
	Column string
	Detail string
	Cause  error
}

func (e *ForeignKeyViolationError) Error() string {
	return fmt.Sprintf("cannot add foreign key on %q: existing data violates it (%s)", e.Column, e.Detail)
}

func (e *ForeignKeyViolationError) Unwrap() error { return e.Cause }

// EnumValueInUseError represents a rejected narrowing of an enum column's
// allowed values: at least one value being removed is still held by existing
// rows. Counts maps each offending value to its exact row count.
// Error() is safe to expose to end users; Cause carries the original
// error (e.g. from pgx) for server-side logging only, reachable via Unwrap.
type EnumValueInUseError struct {
	Column string
	Counts map[string]int
	Cause  error
}

func (e *EnumValueInUseError) Error() string {
	values := make([]string, 0, len(e.Counts))
	for v := range e.Counts {
		values = append(values, v)
	}
	// Map iteration order is random; sort so the message is deterministic.
	sort.Strings(values)

	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q is used by %d row(s)", v, e.Counts[v])
	}

	return fmt.Sprintf("cannot remove value(s) from %q: %s", e.Column, strings.Join(parts, ", "))
}

func (e *EnumValueInUseError) Unwrap() error { return e.Cause }
