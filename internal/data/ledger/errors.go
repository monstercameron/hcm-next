package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Conflict and validation codes. They are stable identifiers that a transport
// layer can map without reading the message text.
const (
	CodeStaleStream          = "CONFLICT_STALE_STREAM"
	CodeIdempotencyConflict  = "CONFLICT_IDEMPOTENCY_KEY_REUSED"
	CodeStreamNotFound       = "STREAM_NOT_REGISTERED"
	CodeInvalidAssertion     = "INVALID_ASSERTION_CLASS"
	CodeAuthorityNotAssigned = "AUTHORITY_NOT_ASSIGNED"
	CodeCorrectionTarget     = "CORRECTION_TARGET_REQUIRED"
	CodePayloadReference     = "PAYLOAD_REFERENCE_INVALID"
	CodePayloadTooLarge      = "PAYLOAD_TOO_LARGE"
	CodeRequestInvalid       = "APPEND_REQUEST_INVALID"
)

// ErrStaleStream reports that the expected head no longer matches the stream.
// It carries both sequences so the caller can decide whether to replan.
type ErrStaleStream struct {
	Tenant    uuid.UUID
	StreamKey string
	Expected  int64
	Actual    int64
}

// Code returns the stable conflict identifier.
func (ErrStaleStream) Code() string { return CodeStaleStream }

func (e ErrStaleStream) Error() string {
	return fmt.Sprintf("%s: stream %s expected head %d but the head is %d",
		CodeStaleStream, e.StreamKey, e.Expected, e.Actual)
}

// ErrIdempotencyConflict reports that an idempotency key was reused with
// different bytes. The earlier assertion stands; the new one is refused.
type ErrIdempotencyConflict struct {
	StreamKey      string
	IdempotencyKey string
	RecordedDigest string
	RequestDigest  string
}

// Code returns the stable conflict identifier.
func (ErrIdempotencyConflict) Code() string { return CodeIdempotencyConflict }

func (e ErrIdempotencyConflict) Error() string {
	return fmt.Sprintf("%s: idempotency key %q on stream %s was recorded with digest %s but this request digests to %s",
		CodeIdempotencyConflict, e.IdempotencyKey, e.StreamKey, e.RecordedDigest, e.RequestDigest)
}

// ErrStreamNotFound reports an append to a stream that was never registered.
type ErrStreamNotFound struct {
	Tenant    uuid.UUID
	StreamKey string
}

// Code returns the stable conflict identifier.
func (ErrStreamNotFound) Code() string { return CodeStreamNotFound }

func (e ErrStreamNotFound) Error() string {
	return fmt.Sprintf("%s: stream %s is not registered for tenant %s",
		CodeStreamNotFound, e.StreamKey, e.Tenant)
}

// ErrInvalidAssertionClass reports an assertion class outside the five declared.
type ErrInvalidAssertionClass struct {
	AssertionClass string
}

// Code returns the stable validation identifier.
func (ErrInvalidAssertionClass) Code() string { return CodeInvalidAssertion }

func (e ErrInvalidAssertionClass) Error() string {
	return fmt.Sprintf("%s: %q is not one of TRANSACTION_FACT, DOMAIN_FACT, EXTERNAL_OBSERVATION, CLAIM, CORRECTION",
		CodeInvalidAssertion, e.AssertionClass)
}

// ErrAuthorityNotAssigned reports that an authority-bearing assertion cited an
// authority that has no assignment covering the effective instant. Recording an
// assertion never implies authority over its subject.
type ErrAuthorityNotAssigned struct {
	AssertionClass AssertionClass
	AuthorityRef   string
	EffectiveAt    time.Time
}

// Code returns the stable validation identifier.
func (ErrAuthorityNotAssigned) Code() string { return CodeAuthorityNotAssigned }

func (e ErrAuthorityNotAssigned) Error() string {
	if e.AuthorityRef == "" {
		return fmt.Sprintf("%s: %s requires an authority assignment", CodeAuthorityNotAssigned, e.AssertionClass)
	}
	return fmt.Sprintf("%s: authority %q has no assignment covering %s for %s",
		CodeAuthorityNotAssigned, e.AuthorityRef, e.EffectiveAt.Format(time.RFC3339), e.AssertionClass)
}

// ErrCorrectionTarget reports a correction without a target, or a target on an
// assertion that is not a correction.
type ErrCorrectionTarget struct {
	AssertionClass AssertionClass
	Reason         string
}

// Code returns the stable validation identifier.
func (ErrCorrectionTarget) Code() string { return CodeCorrectionTarget }

func (e ErrCorrectionTarget) Error() string {
	return fmt.Sprintf("%s: %s %s", CodeCorrectionTarget, e.AssertionClass, e.Reason)
}

// ErrPayloadReference reports that an event carried both a payload and an
// artifact reference, or neither.
type ErrPayloadReference struct {
	Reason string
}

// Code returns the stable validation identifier.
func (ErrPayloadReference) Code() string { return CodePayloadReference }

func (e ErrPayloadReference) Error() string {
	return fmt.Sprintf("%s: %s", CodePayloadReference, e.Reason)
}

// ErrPayloadTooLarge reports bytes that must be stored as a governed artifact.
type ErrPayloadTooLarge struct {
	Length int
	Limit  int
}

// Code returns the stable validation identifier.
func (ErrPayloadTooLarge) Code() string { return CodePayloadTooLarge }

func (e ErrPayloadTooLarge) Error() string {
	return fmt.Sprintf("%s: payload of %d bytes exceeds the %d byte envelope limit; use an artifact reference",
		CodePayloadTooLarge, e.Length, e.Limit)
}

// ErrRequestInvalid reports a missing or malformed field on an append request.
type ErrRequestInvalid struct {
	Field  string
	Reason string
}

// Code returns the stable validation identifier.
func (ErrRequestInvalid) Code() string { return CodeRequestInvalid }

func (e ErrRequestInvalid) Error() string {
	return fmt.Sprintf("%s: %s %s", CodeRequestInvalid, e.Field, e.Reason)
}
