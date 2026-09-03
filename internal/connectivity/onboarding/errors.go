package onboarding

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidManifest reports a structurally invalid manifest, budget,
	// reference pin or reviewer decision.
	ErrInvalidManifest = errors.New("onboarding: manifest is structurally invalid")
	// ErrSignatureInvalid reports a manifest signature that does not verify
	// against the supplied public key: wrong key, corrupted, forged, or
	// absent.
	ErrSignatureInvalid = errors.New("onboarding: manifest signature does not verify")
	// ErrManifestTampered reports a manifest whose recorded digest no longer
	// matches its recomputed content.
	ErrManifestTampered = errors.New("onboarding: manifest content no longer matches its recorded digest")
	// ErrRejected is the ONBOARD-001 preflight refusal. It is never
	// retryable without changing the manifest, the connection or the live
	// connector: an identical preflight of the same three inputs rejects
	// identically.
	ErrRejected = errors.New("onboarding: preflight rejected the manifest")
	// ErrSnapshotChanged reports that the connector's source snapshot no
	// longer matches the one a stored checkpoint addresses. Extraction
	// refuses to resume; the caller decides whether to restart under a new
	// snapshot.
	ErrSnapshotChanged = errors.New("onboarding: source snapshot changed since the last checkpoint")
	// ErrBudgetExceeded reports that an onboarding job's own resource budget
	// - pages, records, bytes or wall time - is exhausted. It is never an
	// external failure: [observe.IsExternalFailure] reports false for it.
	ErrBudgetExceeded = errors.New("onboarding: resource budget exhausted")
	// ErrStalePin reports that a loaded crosswalk snapshot does not match the
	// version and digest the manifest pinned.
	ErrStalePin = errors.New("onboarding: reference or crosswalk snapshot does not match its pin")
	// ErrInvalidReview reports a reviewer decision that cannot be recorded:
	// wrong outcome, missing reviewer, or a canonical id the adjudication
	// never presented as a candidate.
	ErrInvalidReview = errors.New("onboarding: reviewer decision is not valid for this adjudication")
	// ErrNotFound reports a lookup - a crosswalk snapshot, a checkpoint - that
	// has nothing at the requested key.
	ErrNotFound = errors.New("onboarding: not found")
)

// CodeManifestRejected is the caller-facing rejection code
// [Preflight.Run] returns whenever a manifest cannot activate. Its wire form
// is stable so an operator surface can switch on it without parsing prose.
const CodeManifestRejected = "ONBOARD_001_REJECTED"

// Error is the single structured error type this package returns.
//
// Field, State and Version single out the one offending fact a rejection
// turned on - never all three at once, but whichever one the failing check
// was about - so a caller building a diagnostic never has to reparse Detail
// to find what to show an operator.
type Error struct {
	// Op names the operation, e.g. "onboarding.Preflight.Run".
	Op string
	// Code is set on a [Preflight] rejection to [CodeManifestRejected]; it is
	// empty for every other error this package returns.
	Code string
	// Cause is the sentinel this error wraps.
	Cause error
	// Field names the offending manifest, connection or record field, when
	// applicable.
	Field string
	// State names the offending observed state (a lifecycle state, a verify
	// status, a live schema version), when applicable.
	State string
	// Version names the offending observed or expected version, when
	// applicable.
	Version string
	// Detail is a human-readable explanation.
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

// rejected builds the ONBOARD-001 preflight refusal, naming the one
// offending field/state/version the failing check was about.
func rejected(op, field, state, version, format string, args ...any) *Error {
	return &Error{
		Op:      op,
		Code:    CodeManifestRejected,
		Cause:   ErrRejected,
		Field:   field,
		State:   state,
		Version: version,
		Detail:  fmt.Sprintf(format, args...),
	}
}
