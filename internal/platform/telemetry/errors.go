package telemetry

import "fmt"

// RejectionError is the one structured rejection this package returns for
// every field-level validation failure, across Resource, context,
// Envelope, the metric/dashboard/alert catalog and the completeness
// checker. It names the exact offending field, state and schema/policy
// version so a caller never has to parse prose to react to a rejection
// (planning/todos.md OBS-001 GREEN: "return a field-level rejection before
// prohibited content reaches any handler/exporter"; the `<CODE>_REJECTED`
// shape mirrors the convention OBS-003/006/008 name for a seeded-defect
// primary test).
type RejectionError struct {
	// Code is a stable machine code such as "OBS_001_REJECTED" or
	// "OBS_006_REJECTED".
	Code string
	// Field is the exact dotted field path that failed, e.g.
	// "resource.cell_id" or "attributes.password".
	Field string
	// State names why the field failed, e.g. "missing", "prohibited_class",
	// "unknown_key" or "unknown_version".
	State string
	// Version is the schema or policy version the rejection was evaluated
	// against.
	Version int

	err error
}

func newRejection(code, field, state string, version int, err error) *RejectionError {
	return &RejectionError{Code: code, Field: field, State: state, Version: version, err: err}
}

// Error implements error.
func (e *RejectionError) Error() string {
	return fmt.Sprintf("%s: field %q state %q (schema/policy version %d): %v", e.Code, e.Field, e.State, e.Version, e.err)
}

// Unwrap exposes the underlying sentinel so callers can still match with
// errors.Is against the field-class error (ErrResourceField,
// ErrAttributeProhibited, ...) without parsing Code.
func (e *RejectionError) Unwrap() error { return e.err }
