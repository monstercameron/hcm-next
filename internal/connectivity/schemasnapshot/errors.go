package schemasnapshot

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrIncomplete reports a request or record missing required attribution.
	ErrIncomplete = errors.New("schemasnapshot: structurally incomplete")
	// ErrInvalid reports a structurally invalid value that is not merely
	// incomplete (an unrecognized format, an illegal state).
	ErrInvalid = errors.New("schemasnapshot: invalid input")
	// ErrImmutable reports an attempt to change identity metadata, or a
	// decision, that is already on file under the same snapshot identity.
	ErrImmutable = errors.New("schemasnapshot: snapshot is immutable")
	// ErrNotFound reports an unknown snapshot.
	ErrNotFound = errors.New("schemasnapshot: snapshot not found")
	// ErrNotAdmitted reports a snapshot used as a mapping input while it is
	// not (or no longer, or not yet) ADMITTED.
	ErrNotAdmitted = errors.New("schemasnapshot: snapshot is not admitted")
	// ErrStore reports a storage-layer failure.
	ErrStore = errors.New("schemasnapshot: store failed")
)

// Error is the single error type this package returns.
type Error struct {
	Op     string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Detail != "" {
		msg = msg + ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}
