package humanwork

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidAttestation reports an attestation statement that cannot be
	// published. Publication is fail-closed: incomplete statements never
	// become selectable versions.
	ErrInvalidAttestation = errors.New("humanwork: invalid attestation statement")

	// ErrInvalidExpression reports a resolution expression that cannot be
	// compiled: an unnamed role, a pinned person with no policy reference, a
	// missing or malformed scope, a combinator with too few children, a quorum
	// larger than its child set, or a tree past the declared depth/node bound.
	ErrInvalidExpression = errors.New("humanwork: invalid resolution expression")

	// ErrInvalidRequirement reports an ApprovalRequirement that cannot be
	// compiled: a missing scope, cardinality, quorum, separation-of-duties
	// constraint, deadline/expiry or invalidator set.
	ErrInvalidRequirement = errors.New("humanwork: invalid approval requirement")

	// ErrTierBlocked reports a derivation from an approval tier that does not
	// state a requirement at all. UNKNOWN_BLOCKED is the rules engine saying an
	// input was unresolved; it is never quietly read as "no approval required".
	ErrTierBlocked = errors.New("humanwork: approval tier is blocked pending input resolution")

	// ErrInvalidDelegation reports a delegation with no identity, no bounded
	// scope or no expiry. A delegation that never expires is not a delegation.
	ErrInvalidDelegation = errors.New("humanwork: invalid delegation")

	// ErrInvalidResolution reports a resolution request that cannot run: no
	// directory, no effective time, or no requester.
	ErrInvalidResolution = errors.New("humanwork: invalid resolution request")

	// ErrDirectory reports a failure inside the org/authority port itself, as
	// distinct from a directory that ran and returned nobody.
	ErrDirectory = errors.New("humanwork: directory lookup failed")
)

// Error is the typed error this package returns. Op names the operation, Field
// names the offending field path when there is one, and Cause is the sentinel
// to classify against.
type Error struct {
	Op     string
	Field  string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Field != "" {
		msg += " [" + e.Field + "]"
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op, field string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Field: field, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}
