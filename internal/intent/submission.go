package intent

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Submission-transition causes. Classify with [errors.Is]; never by matching
// strings.
var (
	// ErrSubmitNotSimulated reports an intent whose current RequestState is
	// neither SIMULATED (the one state SIMULATED->SUBMITTED starts from) nor
	// already SUBMITTED (the idempotent replay case [Submit] itself handles).
	// A DRAFT or PREFLIGHTED intent has never been simulated and carries no
	// proposal a submission could bind.
	ErrSubmitNotSimulated = errors.New("intent: submit requires a simulated proposal")

	// ErrSubmitAlreadyTerminal reports an attempt to submit an intent whose
	// RequestState has already left the submit/approve path for good
	// (CANCELLED, SUPERSEDED, REJECTED, WITHDRAWN or CLOSED).
	ErrSubmitAlreadyTerminal = errors.New("intent: intent is already in a terminal request state")
)

// SubmitRequest binds the exact simulated proposal a submission commits to.
//
// ProposalRevisionID is the caller's presented identity. The kernel does not
// compare it against anything: P1A mints exactly one deterministic proposal
// revision per intent (its identity is derived from the intent id and the
// canonical request digest, never freshly minted), so verifying "the exact
// same content the caller simulated" is a re-simulation the caller of
// [Submit] performs before calling it, not a comparison this function could
// make from the stored envelope alone.
type SubmitRequest struct {
	ProposalRevisionID string
	RecordedAt         time.Time
}

// submitPath names the RequestState hops [Submit] walks to reach SUBMITTED
// from each starting state it accepts. P1A's SimulateIntent computes and
// answers but persists nothing (its own doc comment: "it writes nothing at
// all"), so no earlier call ever moves a stored instance's RequestState past
// DRAFT; Submit is therefore the one governed write that both persists the
// fact this exact content was preflighted and simulated (the re-simulation
// its caller already performed to derive req.ProposalRevisionID) and binds
// the resulting proposal for submission, in one call. Every hop is a
// separately declared edge under the definition's own lifecycle profile —
// DRAFT never jumps straight to SUBMITTED — so a definition that has not
// declared one of these edges refuses at that hop, not silently.
var submitPath = map[lifecycle.RequestState][]lifecycle.RequestState{
	lifecycle.RequestDraft:       {lifecycle.RequestPreflighted, lifecycle.RequestSimulated, lifecycle.RequestSubmitted},
	lifecycle.RequestPreflighted: {lifecycle.RequestSimulated, lifecycle.RequestSubmitted},
	lifecycle.RequestSimulated:   {lifecycle.RequestSubmitted},
}

// Submit moves instance to SUBMITTED, binding req's proposal revision, and
// starts once.
//
// It is idempotent by construction rather than by a stored comparison: an
// intent already at SUBMITTED is returned unchanged (no transition applied,
// no version bump), because the caller of [Submit] has already verified
// req.ProposalRevisionID names the one proposal revision this intent can
// ever have — a second submission of it is the same fact recorded twice, not
// a second run (EP-INTENT-003 GREEN: "starts once"). A terminal state may
// never be revived by a submit ([ErrSubmitAlreadyTerminal]); any other
// current RequestState with no declared path to SUBMITTED at all
// ([ErrSubmitNotSimulated]) refuses too.
//
// Every dimensional move is applied through [lifecycle.Machine], using this
// definition's own narrowed transition profile
// ([Definition.LifecycleProfiles]) — Submit never assigns instance.Lifecycle
// directly, so an edge this definition has not declared (or a legality rule
// the resulting tuple fails) is refused by the machine, not silently
// accepted. InstanceVersion advances by exactly one for the whole call,
// whatever the hop count: submitting is one governed write, not one per hop.
func Submit(instance *Instance, def Definition, req SubmitRequest) error {
	if instance == nil {
		return fmt.Errorf("%w: instance is nil", ErrInvalidInstance)
	}
	if req.ProposalRevisionID == "" {
		return fmt.Errorf("%w: submit requires a proposal_revision_id", ErrInvalidInstance)
	}
	if req.RecordedAt.IsZero() {
		return fmt.Errorf("%w: submit requires a recorded_at", ErrInvalidInstance)
	}
	if instance.Definition != def.Ref {
		return fmt.Errorf("%w: definition mismatch", ErrInvalidInstance)
	}

	switch instance.Lifecycle.Request {
	case lifecycle.RequestSubmitted:
		// Idempotent replay: the one deterministic proposal revision this
		// intent can ever have was already bound. Nothing moves, nothing is
		// recorded a second time.
		return nil
	case lifecycle.RequestCancelled, lifecycle.RequestSuperseded, lifecycle.RequestRejected,
		lifecycle.RequestWithdrawn, lifecycle.RequestClosed:
		return fmt.Errorf("%w: request state is %s", ErrSubmitAlreadyTerminal, instance.Lifecycle.Request)
	}
	hops, ok := submitPath[instance.Lifecycle.Request]
	if !ok {
		return fmt.Errorf("%w: request state is %s", ErrSubmitNotSimulated, instance.Lifecycle.Request)
	}

	ctx := instance.LifecycleContext(def)
	m, err := lifecycle.NewMachine(def.LifecycleProfiles(), instance.Lifecycle, ctx)
	if err != nil {
		return err
	}
	now := values.NewInstant(req.RecordedAt)
	for _, hop := range hops {
		next := m.Current()
		next.Request = hop
		if err := m.Apply(next, ctx, lifecycle.TransitionRecord{
			At:        now,
			ReasonRef: "intent.submitted",
		}); err != nil {
			return err
		}
	}
	instance.Lifecycle = m.Current()
	instance.InstanceVersion++
	instance.RecordedAt = now
	instance.LastTransitionAt = now
	return nil
}
