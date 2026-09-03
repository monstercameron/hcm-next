package timeauth

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidSample reports a Sample that is structurally unusable: an
	// unset Wall instant, or a SelfReportedHealth value that is not one of
	// the declared Health constants.
	ErrInvalidSample = errors.New("timeauth: invalid sample")

	// ErrNilClock reports a Monitor constructed with a nil Clock.
	ErrNilClock = errors.New("timeauth: clock is nil")

	// ErrInvalidThresholds reports Options whose MaxSkew is below its
	// SoftSkew, or whose MaxUncertainty is below its SoftUncertainty.
	ErrInvalidThresholds = errors.New("timeauth: invalid monitor thresholds")

	// ErrMonotonicRegression reports a Sample whose Monotonic reading moved
	// backward relative to the previous sample taken from the same Clock.
	// This is a broken source contract, not a business condition Evidence
	// can classify: Observe refuses to produce evidence at all rather than
	// pretend monotonic sequencing held.
	ErrMonotonicRegression = errors.New("timeauth: monotonic reading moved backward")

	// ErrTimeUntrusted is the TIME_UNTRUSTED refusal: RequireTrusted returns
	// it whenever Observe's health verdict is UNTRUSTED. An effect-bearing
	// operation must treat this as a hard stop.
	ErrTimeUntrusted = errors.New("timeauth: TIME_UNTRUSTED")
)

// Error is the single error type this package returns for its own
// diagnosable failures. Unwrap exposes the sentinel cause for [errors.Is].
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
