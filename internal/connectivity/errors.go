package connectivity

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrCredential reports that the connection's credential reference is
	// absent, malformed, expired or rejected by the provider. It is never
	// retryable: a new credential must be supplied out of band.
	ErrCredential = errors.New("connectivity: credential is not usable")

	// ErrPermission reports that the credential authenticated but is not
	// entitled to the object or operation. Retrying cannot help.
	ErrPermission = errors.New("connectivity: permission denied by external system")

	// ErrSchema reports that the provider returned a shape this connector
	// version does not understand, or a schema version other than the one the
	// page sequence started under. Schema drift mid-run invalidates the run.
	ErrSchema = errors.New("connectivity: external schema is not understood")

	// ErrTransient reports a failure that a later identical read may survive:
	// timeouts, throttling, partial responses, provider 5xx.
	ErrTransient = errors.New("connectivity: transient external failure")

	// ErrBounds reports a request that exceeds the negotiated read bounds.
	// It is a caller defect, not a provider one.
	ErrBounds = errors.New("connectivity: request exceeds declared bounds")

	// ErrCursor reports an unusable continuation token: expired, forged, or
	// minted against a different source snapshot.
	ErrCursor = errors.New("connectivity: pagination cursor is not usable")

	// ErrUnsupported reports an object or read mode the connector version does
	// not declare.
	ErrUnsupported = errors.New("connectivity: operation is not supported by this connector version")

	// ErrInvalid reports a structurally invalid definition, connection or
	// request.
	ErrInvalid = errors.New("connectivity: invalid input")

	// ErrImmutable reports an attempt to change something already published.
	ErrImmutable = errors.New("connectivity: published record is immutable")

	// ErrNotFound reports an unknown definition, version or connection.
	ErrNotFound = errors.New("connectivity: not found")

	// ErrIllegalTransition reports a lifecycle transition the legality table
	// does not permit.
	ErrIllegalTransition = errors.New("connectivity: illegal lifecycle transition")

	// ErrCapabilityBroadened reports a connection claiming a capability its
	// definition version does not publish.
	ErrCapabilityBroadened = errors.New("connectivity: connection broadens definition capabilities")
)

// Class is the coarse classification a caller switches on. It exists so that
// retry, alerting and diagnosis policy is written once against the class
// rather than per provider.
type Class string

// The classes. There is no "other": every error this package returns carries
// exactly one of these.
const (
	// ClassCredential means the credential itself must change.
	ClassCredential Class = "CREDENTIAL"
	// ClassPermission means the credential is fine and the grant is not.
	ClassPermission Class = "PERMISSION"
	// ClassSchema means the external shape changed under us.
	ClassSchema Class = "SCHEMA"
	// ClassTransient means an identical retry is legitimate.
	ClassTransient Class = "TRANSIENT"
	// ClassBounds means the caller asked for more than was negotiated.
	ClassBounds Class = "BOUNDS"
	// ClassCursor means the continuation token cannot be honoured.
	ClassCursor Class = "CURSOR"
	// ClassUnsupported means the connector version never claimed this.
	ClassUnsupported Class = "UNSUPPORTED"
	// ClassInvalid means the input is structurally wrong.
	ClassInvalid Class = "INVALID"
	// ClassConflict means a published, immutable record was contradicted.
	ClassConflict Class = "CONFLICT"
)

// Valid reports whether c is one of the declared classes.
func (c Class) Valid() bool {
	switch c {
	case ClassCredential, ClassPermission, ClassSchema, ClassTransient,
		ClassBounds, ClassCursor, ClassUnsupported, ClassInvalid, ClassConflict:
		return true
	default:
		return false
	}
}

// Retryable reports whether an identical retry of the same request is
// meaningful. Only transient failures are; everything else needs a decision.
func (c Class) Retryable() bool { return c == ClassTransient }

// Error is the single error type this package returns.
//
// Detail is written by this package and by connector implementations, and is
// expected to appear in logs and diagnostics. Nothing that could carry secret
// material is ever placed in it: a credential appears as its opaque reference
// or not at all.
type Error struct {
	// Op names the operation, e.g. "connectivity.Registry.Publish".
	Op string
	// Class is the caller-facing classification.
	Class Class
	// Cause is the sentinel this error wraps.
	Cause error
	// Detail is a non-secret explanation.
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

// Retryable reports whether an identical retry is meaningful.
func (e *Error) Retryable() bool { return e.Class.Retryable() }

// ClassOf reports the class of err, and whether err was produced by this
// package at all. A provider error that was never classified must not be
// silently treated as transient, so ok is false rather than a default class.
func ClassOf(err error) (Class, bool) {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Class, true
	}
	return "", false
}

// newError builds a classified error. classOf pins the class to the sentinel
// so a caller can never see, say, a permission sentinel labelled transient.
func newError(op string, cause error, format string, args ...any) *Error {
	return &Error{
		Op:     op,
		Class:  classOf(cause),
		Cause:  cause,
		Detail: fmt.Sprintf(format, args...),
	}
}

// classOf maps a sentinel to its class. The mapping is total over the
// sentinels this package declares.
func classOf(cause error) Class {
	switch {
	case errors.Is(cause, ErrCredential):
		return ClassCredential
	case errors.Is(cause, ErrPermission):
		return ClassPermission
	case errors.Is(cause, ErrSchema):
		return ClassSchema
	case errors.Is(cause, ErrTransient):
		return ClassTransient
	case errors.Is(cause, ErrBounds):
		return ClassBounds
	case errors.Is(cause, ErrCursor):
		return ClassCursor
	case errors.Is(cause, ErrUnsupported):
		return ClassUnsupported
	case errors.Is(cause, ErrImmutable), errors.Is(cause, ErrIllegalTransition),
		errors.Is(cause, ErrCapabilityBroadened):
		return ClassConflict
	default:
		return ClassInvalid
	}
}

// Fail builds a classified error for a connector implementation outside this
// package. Implementations use it so that every connector's failures classify
// identically, which is what makes retry policy provider-independent.
func Fail(op string, cause error, format string, args ...any) *Error {
	return newError(op, cause, format, args...)
}
