package reconcile

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Sentinels. Classify with [errors.Is]; read [CodeOf] for the exact reason.
// Never match message text.
var (
	// ErrReconcile is the sentinel every refusal from this package unwraps to.
	ErrReconcile = errors.New("reconcile: refused")

	// ErrInvalid reports a request that is not internally consistent. It is
	// returned before any statement runs.
	ErrInvalid = errors.New("reconcile: invalid request")

	// ErrNotFound reports a job that does not exist for the tenant.
	ErrNotFound = errors.New("reconcile: job not found")

	// ErrFenceRefused reports a presented lease fence the durable lease did
	// not accept: stale, foreign, or naming a resource nobody holds.
	ErrFenceRefused = errors.New("reconcile: fence refused")

	// ErrAlreadyTerminal reports a poll against a job that has already
	// settled. It is not an error condition for a caller to treat as failure:
	// [Coordinator.Poll] returns it wrapped in a no-op [Polled], never as a
	// bare refusal, but it is exposed so a caller can classify a raw store
	// read the same way.
	ErrAlreadyTerminal = errors.New("reconcile: job already settled")

	// ErrVersionConflict reports a compare-and-swap advance whose expected
	// version is not the stored one: a concurrent caller settled the job
	// first. Nothing was written.
	ErrVersionConflict = errors.New("reconcile: version conflict")

	// ErrObserverFailed reports a failure from the configured [Observer].
	ErrObserverFailed = errors.New("reconcile: observer failed")

	// ErrComparerFailed reports a failure from the configured [Comparer], or a
	// comparer that returned a status this package does not accept.
	ErrComparerFailed = errors.New("reconcile: comparer failed")

	// ErrStorage reports a database failure underneath a well-formed request.
	ErrStorage = errors.New("reconcile: storage failed")
)

// Stable refusal codes.
const (
	CodeInvalid          = "INVALID_RECONCILE_REQUEST"
	CodeNotFound         = "JOB_NOT_FOUND"
	CodeFenceRefused     = "FENCE_REFUSED"
	CodeAlreadyTerminal  = "JOB_ALREADY_SETTLED"
	CodeVersionConflict  = "VERSION_CONFLICT"
	CodeObserverFailed   = "OBSERVER_FAILED"
	CodeComparerFailed   = "COMPARER_FAILED"
	CodeStorageFailed    = "STORAGE_FAILED"
	CodeNotMandatory     = "EFFECT_NOT_MANDATORY"
	CodeUnexpectedStatus = "UNEXPECTED_VERDICT_STATUS"
)

// Error is one typed refusal naming the code, the tenant and job it happened
// to, and why.
type Error struct {
	Code     string
	TenantID uuid.UUID
	JobID    uuid.UUID
	Detail   string

	sentinel error
	err      error
}

func (e *Error) Error() string {
	loc := ""
	if e.TenantID != uuid.Nil {
		loc = " for tenant " + e.TenantID.String()
	}
	if e.JobID != uuid.Nil {
		loc += " (job " + e.JobID.String() + ")"
	}
	msg := fmt.Sprintf("reconcile: %s%s: %s", e.Code, loc, e.Detail)
	if e.err != nil {
		msg += ": " + e.err.Error()
	}
	return msg
}

// Unwrap exposes the classifying sentinel, the package sentinel and the
// underlying cause.
func (e *Error) Unwrap() []error {
	out := []error{ErrReconcile}
	if e.sentinel != nil {
		out = append(out, e.sentinel)
	}
	if e.err != nil {
		out = append(out, e.err)
	}
	return out
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code string, sentinel error, tenantID, jobID uuid.UUID, format string, args ...any) *Error {
	return &Error{
		Code: code, TenantID: tenantID, JobID: jobID,
		Detail: fmt.Sprintf(format, args...), sentinel: sentinel,
	}
}

func wrapErr(code string, sentinel error, tenantID, jobID uuid.UUID, cause error, format string, args ...any) *Error {
	e := refuse(code, sentinel, tenantID, jobID, format, args...)
	e.err = cause
	return e
}

func invalid(tenantID, jobID uuid.UUID, format string, args ...any) *Error {
	return refuse(CodeInvalid, ErrInvalid, tenantID, jobID, format, args...)
}
