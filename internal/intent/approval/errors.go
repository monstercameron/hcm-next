package approval

import (
	"errors"
	"fmt"
)

// Stable refusal codes. They are the tokens the todo contracts name, exposed so
// a caller can branch on the refusal without matching an error string.
const (
	// CodeInvalidProposal is returned when a vote names a proposal, digest,
	// control context or task version that is not the one being decided.
	CodeInvalidProposal = "INVALID_PROPOSAL"
	// CodeAuthorityChanged is returned when the voter is not in the current
	// candidate set for the requirement, or is claiming a route into it they
	// do not hold.
	CodeAuthorityChanged = "AUTHORITY_CHANGED"
	// CodeConflictingDecision is returned when a principal submits a second,
	// different decision for a requirement they have already decided.
	CodeConflictingDecision = "CONFLICTING_DECISION"
	// CodeUnknownRequirement is returned for a vote on a requirement the bound
	// requirement set does not contain.
	CodeUnknownRequirement = "UNKNOWN_REQUIREMENT"
	// CodeInvalidBinder is returned when the binder itself cannot be built.
	CodeInvalidBinder = "INVALID_BINDER"
	// CodeInvalidAssessment is returned when a materiality assessment cannot
	// run at all, as distinct from one that ran and returned BLOCKED.
	CodeInvalidAssessment = "INVALID_ASSESSMENT"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidProposal reports a decision submitted against anything but the
	// exact bound proposal revision and its control context.
	ErrInvalidProposal = errors.New("approval: " + CodeInvalidProposal)

	// ErrAuthorityChanged reports a decision from a principal the current
	// resolution does not authorize.
	ErrAuthorityChanged = errors.New("approval: " + CodeAuthorityChanged)

	// ErrConflictingDecision reports a second, differing decision from a
	// principal who has already decided. A prior vote is never mutated;
	// reapproval creates a new decision on a new revision.
	ErrConflictingDecision = errors.New("approval: " + CodeConflictingDecision)

	// ErrUnknownRequirement reports a vote on a requirement outside the bound
	// requirement set.
	ErrUnknownRequirement = errors.New("approval: " + CodeUnknownRequirement)

	// ErrInvalidBinder reports a binder that cannot be constructed: no minted
	// proposal digest, a resolution for an unknown requirement, or a
	// requirement with no server-held rendered projection.
	ErrInvalidBinder = errors.New("approval: " + CodeInvalidBinder)

	// ErrInvalidAssessment reports a materiality assessment that cannot run.
	ErrInvalidAssessment = errors.New("approval: " + CodeInvalidAssessment)
)

// Error is the typed error this package returns.
type Error struct {
	Op     string
	Field  string
	Code   string
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

// CodeOf returns the stable refusal code carried by err, or "" when err is not
// one of this package's errors.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func newError(op, field, code string, cause error, format string, args ...any) *Error {
	return &Error{
		Op: op, Field: field, Code: code, Cause: cause,
		Detail: fmt.Sprintf(format, args...),
	}
}
