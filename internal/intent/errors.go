package intent

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidReference reports a malformed definition reference: an
	// unqualified id, a free-form display name, or a zero version.
	ErrInvalidReference = errors.New("intent: invalid definition reference")

	// ErrInvalidDefinition reports a definition that cannot be compiled into
	// the registry: a missing DRAFT_CONTRACT field, an unknown schema or
	// capability, a family outside the three kernel families, an invalid
	// family/side-effect pairing, or a population scope on a non-CHANGE_REQUEST.
	ErrInvalidDefinition = errors.New("intent: invalid intent definition")

	// ErrDuplicateDefinition reports material reuse of an (intent_type_id,
	// version) pair. Published versions are immutable.
	ErrDuplicateDefinition = errors.New("intent: duplicate definition version")

	// ErrUnknownDefinition reports a reference the registry does not publish.
	ErrUnknownDefinition = errors.New("intent: unknown definition")

	// ErrNotInvocable reports a definition that resolves but may not be
	// instantiated: below DRAFT_CONTRACT maturity, or retired.
	ErrNotInvocable = errors.New("intent: definition is not invocable")

	// ErrMissingNegativeStatePolicy reports a definition that declares an
	// applicable negative state without a policy that decides it.
	ErrMissingNegativeStatePolicy = errors.New("intent: applicable negative state has no policy")

	// ErrInvalidNegativeStatePolicy reports a policy whose action is not one of
	// the eight declared actions, or that omits required evidence, expiry or
	// revalidation semantics.
	ErrInvalidNegativeStatePolicy = errors.New("intent: invalid negative-state policy")

	// ErrInvalidInstance reports an envelope missing a required field:
	// tenant, organization scope, purpose, initiator, definition, idempotency
	// key, correlation id, classification, retention class or control snapshots.
	ErrInvalidInstance = errors.New("intent: invalid intent instance")

	// ErrUntypedPayload reports a request or proposal payload that is not a
	// typed Protobuf payload with a resolvable schema reference. Arbitrary
	// JSON, a map, or a model prompt is never an authoritative payload.
	ErrUntypedPayload = errors.New("intent: request payload is not typed")

	// ErrPreflight reports a preflight that could not run at all, as distinct
	// from one that ran and returned BLOCKED or DENIED.
	ErrPreflight = errors.New("intent: preflight failed")

	// ErrEffectInPreflight reports a domain preflight that declared an effect.
	// Preflight reads; it never mutates and never causes an external effect.
	ErrEffectInPreflight = errors.New("intent: preflight declared an effect")

	// ErrInvalidProposal reports a proposal revision that cannot be minted: a
	// caller-supplied digest, a missing control/source/reference version, or a
	// material child intent that the revision does not declare.
	ErrInvalidProposal = errors.New("intent: invalid proposal revision")

	// ErrProposalImmutable reports an attempt to edit or replace a recorded
	// proposal revision. A revision is superseded by a new revision, never
	// edited in place.
	ErrProposalImmutable = errors.New("intent: proposal revisions are immutable")

	// ErrHiddenChildIntent reports a material child intent observed in the
	// proposal content that the revision does not bind.
	ErrHiddenChildIntent = errors.New("intent: hidden material child intent")

	// ErrInvalidPlan reports a transaction plan missing a required
	// declaration: participant, read, write, effect, sequence, idempotency,
	// approval, revalidation, compensation or observation.
	ErrInvalidPlan = errors.New("intent: invalid transaction plan")

	// ErrExecutionProhibitedGateA reports an attempt to gain execution
	// authority for a plan in Gate A. Its Code is EXECUTION_PROHIBITED_GATE_A.
	ErrExecutionProhibitedGateA = errors.New("intent: EXECUTION_PROHIBITED_GATE_A")

	// ErrModeNotAllowed reports an execution mode the definition does not
	// allow, including EXECUTE for any P1A definition.
	ErrModeNotAllowed = errors.New("intent: execution mode is not allowed")

	// ErrInitiatorNotAllowed reports an initiator kind the definition does not
	// allow. Agent initiation never increases authority.
	ErrInitiatorNotAllowed = errors.New("intent: initiator kind is not allowed")
)

// Error is the typed error this package returns. Op names the operation,
// Field names the offending field path when there is one, and Cause is the
// sentinel to classify against.
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
