package intent

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Cancellation-transition causes. Classify with [errors.Is]; never by
// matching strings.
var (
	// ErrCancelAlreadyTerminal reports an attempt to cancel an intent whose
	// RequestState has already left the live path for good.
	ErrCancelAlreadyTerminal = errors.New("intent: intent is already in a terminal request state")

	// ErrCancelRepairRefRequired reports a [CancellationPointPartialEffect]
	// disposition presented with no repair reference: REPAIR_REQUIRED is a
	// kernel legality rule (RuleCommittedRequiresReceipt), never a status
	// asserted without the record it requires.
	ErrCancelRepairRefRequired = errors.New("intent: a partial-effect cancellation requires a repair reference")

	// ErrCancelUnrecognizedPoint reports a [CancellationPoint] value this
	// kernel does not know, so an unexhausted future value fails closed
	// instead of falling through to a guess.
	ErrCancelUnrecognizedPoint = errors.New("intent: unrecognized cancellation point")
)

// CancellationPoint is what [CancelInstance] needs to know about a workflow
// instance's current execution position before it may act, from
// internal/workflow/compile.go's safe-point contract (WF-RUN-010): a running
// workflow may only be cleanly cancelled, or found to need repair, AT a
// declared safe point. Asked while execution is inside an atomic region
// between safe points — or asked with no execution facts available at all —
// the honest answer is "not yet decided", never a guess that happens to
// look like success.
type CancellationPoint uint8

const (
	// CancellationPointUnknown means no execution facts are available: no
	// [internal/intent/app.SafePoints] port is configured, or no workflow
	// instance is bound to this intent's execution at all. [CancelInstance] treats
	// it exactly like [CancellationPointMidFlight] — it never claims a clean
	// cancel it cannot prove.
	CancellationPointUnknown CancellationPoint = iota
	// CancellationPointMidFlight reports that the instance is between safe
	// points, inside an atomic execution region right now. Cancellation is
	// requested but not yet decided.
	CancellationPointMidFlight
	// CancellationPointClean reports that the instance is stopped at a
	// declared safe point and no effect from the current atomic region has
	// landed. Cancellation completes.
	CancellationPointClean
	// CancellationPointPartialEffect reports that the instance is stopped at
	// a declared safe point, but the atomic region already committed a
	// partial effect that cancellation cannot cleanly reverse. Repair is
	// required.
	CancellationPointPartialEffect
)

func (p CancellationPoint) valid() bool {
	switch p {
	case CancellationPointUnknown, CancellationPointMidFlight, CancellationPointClean, CancellationPointPartialEffect:
		return true
	default:
		return false
	}
}

// CancellationDisposition is the truthful GREEN answer [CancelInstance] reports.
//
// There is no wire enum for it (schema/proto/hcmnext/intents/v1's
// CancelIntentResponse carries only an IntentInstance): the disposition is
// told entirely through the dimensional state the returned instance carries,
// and this type exists so the mapping from dimensions to meaning is named
// and tested in exactly one place rather than re-derived by every caller.
type CancellationDisposition string

const (
	// DispositionCancelled: the request is cancelled and no execution effect
	// will follow. RequestState moves to CANCELLED.
	DispositionCancelled CancellationDisposition = "CANCELLED"
	// DispositionCancellationPending: cancellation was requested but the
	// runtime has not yet reached a point where it can be decided. No
	// dimension moves — claiming CANCELLED here would be exactly the lie
	// this todo's clause forbids.
	DispositionCancellationPending CancellationDisposition = "CANCELLATION_PENDING"
	// DispositionTooLate: the business effect already committed. RequestState
	// is left exactly as it was; saying CANCELLED would falsify the record.
	DispositionTooLate CancellationDisposition = "TOO_LATE"
	// DispositionRepairRequired: a partial, non-reversible effect landed.
	// ExecutionState moves to REPAIR_REQUIRED; RequestState is left as it
	// was, because the request was neither cleanly cancelled nor completed.
	DispositionRepairRequired CancellationDisposition = "REPAIR_REQUIRED"
)

// CancelRequest is the caller's cancellation ask plus the execution facts
// [internal/intent/app.IntentService.CancelIntent] resolved before calling
// [CancelInstance]. The kernel never resolves Point itself: that answer comes from
// the workflow runtime's own safe-point bookkeeping, which this package does
// not have access to and must not guess at.
type CancelRequest struct {
	// ReasonRef is the caller's stated governed reason. It is threaded
	// through as the transition's GovernanceDecisionRef, which satisfies the
	// declared edges this definition marks RequiresGovernance (for example
	// APPROVED->CANCELLED) and is otherwise inert.
	ReasonRef string
	// Point is only consulted when ExecutionState is EXECUTING; every other
	// ExecutionState decides its own disposition without asking the runtime
	// a safe-point question that does not apply to it.
	Point CancellationPoint
	// RepairRef names the RepairPlan or incident a [DispositionRepairRequired]
	// outcome binds. Required exactly when the returned disposition is
	// DispositionRepairRequired.
	RepairRef  string
	RecordedAt time.Time
}

// Cancel evaluates the current phase and cancellation policy and returns the
// truthful disposition, exactly as
// planning/specs/workflow-runtime.md#pause-cancellation-and-propagation
// describes: cleanly cancellable -> CANCELLED, effects need reversal ->
// REPAIR_REQUIRED, cannot yet be decided -> CANCELLATION_PENDING, already
// committed -> TOO_LATE (a fifth workflow-runtime outcome, "cannot cancel ->
// supersede", is SupersedeOriginal's job, not this one).
//
// instance is mutated only for [DispositionCancelled] and
// [DispositionRepairRequired]: those are the two dispositions under which
// something about the intent's own state actually changed.
// [DispositionCancellationPending] and [DispositionTooLate] leave every
// dimension exactly as it was — nothing happened yet, or something already
// final happened that this call did not cause — so instance.Lifecycle,
// InstanceVersion and every linked record are untouched.
//
// Every dimensional move this function makes goes through
// [lifecycle.Machine] against this definition's own narrowed transition
// profile: an edge the definition has not declared, or a resulting tuple a
// fixed legality rule refuses, is refused by the machine, never hand-rolled
// here.
func CancelInstance(instance *Instance, def Definition, req CancelRequest) (CancellationDisposition, error) {
	if instance == nil {
		return "", fmt.Errorf("%w: instance is nil", ErrInvalidInstance)
	}
	if req.RecordedAt.IsZero() {
		return "", fmt.Errorf("%w: cancel requires a recorded_at", ErrInvalidInstance)
	}
	if instance.Definition != def.Ref {
		return "", fmt.Errorf("%w: definition mismatch", ErrInvalidInstance)
	}
	if !req.Point.valid() {
		return "", fmt.Errorf("%w: %d", ErrCancelUnrecognizedPoint, req.Point)
	}

	switch instance.Lifecycle.Request {
	case lifecycle.RequestCancelled, lifecycle.RequestSuperseded, lifecycle.RequestRejected,
		lifecycle.RequestWithdrawn, lifecycle.RequestClosed:
		return "", fmt.Errorf("%w: request state is %s", ErrCancelAlreadyTerminal, instance.Lifecycle.Request)
	}

	// Every ExecutionState value is handled by name. A switch that fell
	// through to a default "looks cancellable" branch is exactly the near-
	// match mapping bug class this todo warns about: TOO_LATE and
	// REPAIR_REQUIRED must never be reachable by a value this function did
	// not explicitly recognize.
	switch instance.Lifecycle.Execution {
	case lifecycle.ExecutionUnspecified:
		return "", fmt.Errorf("%w: execution state is UNSPECIFIED", ErrInvalidInstance)

	case lifecycle.ExecutionNotPlanned, lifecycle.ExecutionScheduled, lifecycle.ExecutionRevalidating:
		// Nothing has entered an atomic execution region yet (or the runtime
		// is only revalidating before it would); cancellation is clean and
		// the runtime never needs to schedule again.
		return cancelClean(instance, def, req, lifecycle.ExecutionNotPlanned)

	case lifecycle.ExecutionBlocked:
		// Already stopped short of running. Formalize the cancellation
		// without touching an ExecutionState that is not moving.
		return cancelClean(instance, def, req, lifecycle.ExecutionBlocked)

	case lifecycle.ExecutionExecuting:
		switch req.Point {
		case CancellationPointClean:
			// The runtime is inside its execution phase but reports a safe
			// point with nothing landed: BLOCKED is the truthful runtime
			// state (this execution actually ran, unlike NOT_PLANNED, and
			// nothing further will run without an operator), paired with a
			// real RequestState=CANCELLED.
			return cancelClean(instance, def, req, lifecycle.ExecutionBlocked)
		case CancellationPointPartialEffect:
			return cancelRepairRequired(instance, def, req)
		case CancellationPointMidFlight, CancellationPointUnknown:
			// Cancellation is requested but the runtime has not reached a
			// safe point where it can be decided. Nothing moves.
			return DispositionCancellationPending, nil
		default:
			return "", fmt.Errorf("%w: %d", ErrCancelUnrecognizedPoint, req.Point)
		}

	case lifecycle.ExecutionCommitted:
		// The business effect already happened. Saying CANCELLED here is the
		// exact RED clause this todo closes.
		return DispositionTooLate, nil

	case lifecycle.ExecutionRepairRequired:
		// A repair is already outstanding from an earlier fact this call did
		// not cause. Cancelling now does not erase that; the truthful answer
		// is that repair is still required, and the request is left exactly
		// as it stood.
		return DispositionRepairRequired, nil

	default:
		return "", fmt.Errorf("%w: unrecognized execution state %s", ErrInvalidInstance, instance.Lifecycle.Execution)
	}
}

// cancelClean applies RequestState->CANCELLED and, when it differs from the
// instance's current ExecutionState, the declared ExecutionState edge to
// targetExecution.
func cancelClean(instance *Instance, def Definition, req CancelRequest, targetExecution lifecycle.ExecutionState) (CancellationDisposition, error) {
	ctx := instance.LifecycleContext(def)
	m, err := lifecycle.NewMachine(def.LifecycleProfiles(), instance.Lifecycle, ctx)
	if err != nil {
		return "", err
	}
	next := instance.Lifecycle
	next.Request = lifecycle.RequestCancelled
	next.Execution = targetExecution
	now := values.NewInstant(req.RecordedAt)
	if err := m.Apply(next, ctx, lifecycle.TransitionRecord{
		At:                    now,
		ReasonRef:             req.ReasonRef,
		GovernanceDecisionRef: req.ReasonRef,
	}); err != nil {
		return "", err
	}
	instance.Lifecycle = m.Current()
	instance.InstanceVersion++
	instance.RecordedAt = now
	instance.LastTransitionAt = now
	return DispositionCancelled, nil
}

// cancelRepairRequired applies ExecutionState->REPAIR_REQUIRED, leaving
// RequestState exactly as it was: the request was neither cleanly cancelled
// nor completed, so recording either would misstate what actually happened.
func cancelRepairRequired(instance *Instance, def Definition, req CancelRequest) (CancellationDisposition, error) {
	if req.RepairRef == "" {
		return "", ErrCancelRepairRefRequired
	}
	ctx := instance.LifecycleContext(def)
	// RuleCommittedRequiresReceipt reads RepairRef off the context, which is
	// itself read off the instance's stored field; the presented repair
	// reference is the one this legality check must see, whether or not the
	// instance had one already.
	ctx.RepairRef = req.RepairRef
	m, err := lifecycle.NewMachine(def.LifecycleProfiles(), instance.Lifecycle, ctx)
	if err != nil {
		return "", err
	}
	next := instance.Lifecycle
	next.Execution = lifecycle.ExecutionRepairRequired
	now := values.NewInstant(req.RecordedAt)
	if err := m.Apply(next, ctx, lifecycle.TransitionRecord{
		At:                    now,
		ReasonRef:             req.ReasonRef,
		GovernanceDecisionRef: req.ReasonRef,
	}); err != nil {
		return "", err
	}
	instance.Lifecycle = m.Current()
	instance.RepairRef = req.RepairRef
	instance.InstanceVersion++
	instance.RecordedAt = now
	instance.LastTransitionAt = now
	return DispositionRepairRequired, nil
}
