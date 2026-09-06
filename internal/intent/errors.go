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

	// ErrCallerSelectedAuthority reports a request that tried to select its own
	// trusted origin: a principal, tenant, session, delegation chain, identity
	// assurance, producer or provider authority the caller does not get to
	// choose, or an origin kind that disagrees with the verified credential.
	ErrCallerSelectedAuthority = errors.New("intent: caller-selected trusted origin")

	// ErrUntrustedOrigin reports an origin the trusted boundary itself could
	// not substantiate: a schedule, event, repair or operator source with no
	// authenticated producer and no source evidence, an origin kind that
	// contradicts the verified initiator kind, or an integration event that
	// tried to inherit its provider's authority.
	ErrUntrustedOrigin = errors.New("intent: origin is not substantiated")

	// ErrInvalidTemplate reports an intent template that embeds a principal, a
	// tenant, a server-owned fact, an approval, an idempotency key or a stale
	// definition version. A template supplies permitted defaults and nothing
	// else.
	ErrInvalidTemplate = errors.New("intent: invalid intent template")

	// ErrInvalidDraft reports a draft that carries submitted-intent state
	// (identity, lifecycle, approval or evidence) or names inputs its
	// definition does not declare.
	ErrInvalidDraft = errors.New("intent: invalid intent draft")

	// ErrDraftAlreadySubmitted reports an edit to, or a second submission of, a
	// draft that already minted an IntentInstance. Submission is the moment a
	// mutable draft stops being mutable.
	ErrDraftAlreadySubmitted = errors.New("intent: draft is already submitted")

	// ErrInvalidSavedAction reports a saved or favourite action that stored
	// copied authority, a rendered sensitive payload or an unversioned
	// definition reference instead of a reference plus authorized parameters.
	ErrInvalidSavedAction = errors.New("intent: invalid saved action")

	// ErrLineageReuse reports a clone or fork that reused the source's causal
	// identity: its idempotency key, correlation id, approval bindings,
	// evidence, or proposal revisions.
	ErrLineageReuse = errors.New("intent: clone or fork reused source identity")

	// ErrInvalidRelationship reports an intent-to-intent relationship missing a
	// tenant, cause, purpose, ordering or completion policy, or one whose
	// endpoints do not pin exact intent and proposal versions.
	ErrInvalidRelationship = errors.New("intent: invalid intent relationship")

	// ErrRelationshipCycle reports a cycle in a relationship kind that
	// prohibits one.
	ErrRelationshipCycle = errors.New("intent: prohibited relationship cycle")

	// ErrImmutableRelationship reports an attempt to re-parent, re-kind or
	// re-order a recorded relationship. Parentage is immutable; a relationship
	// revision may only refine propagation and completion policy.
	ErrImmutableRelationship = errors.New("intent: intent relationships are immutable")

	// ErrAmbiguousRelationship reports two different relationship kinds
	// recorded for one ordered pair, or a duplicate ordinal under one parent
	// and kind. The nine kinds are never conflated.
	ErrAmbiguousRelationship = errors.New("intent: ambiguous intent relationship")

	// ErrInvalidModeContract reports an execution mode and environment pair
	// that has no contract, or a definition that does not allow the mode.
	ErrInvalidModeContract = errors.New("intent: invalid execution mode contract")

	// ErrEffectEscalation reports an attempt to commit domain truth, cause an
	// external effect or consume a live approval under a mode contract that
	// guarantees none of them.
	ErrEffectEscalation = errors.New("intent: execution mode may not escalate effects")

	// ErrCausalSeparation reports a replay that does not name the historical
	// intent it replays, or a new action that pretends to be one.
	ErrCausalSeparation = errors.New("intent: replay and new action are not causally separated")
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
