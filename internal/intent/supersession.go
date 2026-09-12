package intent

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// Supersession-transition causes. Classify with [errors.Is]; never by
// matching strings.
var (
	// ErrSupersedeAlreadyTerminal reports an attempt to supersede an intent
	// whose RequestState has already left the live path for good.
	ErrSupersedeAlreadyTerminal = errors.New("intent: intent is already in a terminal request state")

	// ErrSupersedeSelf reports a successor identity equal to the original's
	// own id: a supersession links two distinct intents, never one to itself.
	ErrSupersedeSelf = errors.New("intent: an intent may not supersede itself")
)

// SupersessionRecord is the redaction-safe outcome [SupersedeOriginal]
// returns: identifiers and a reason only, exactly like [Lineage] — enough to
// render the link without carrying subject, payload or proposal content a
// second time.
type SupersessionRecord struct {
	SupersededIntentID  string
	SupersedingIntentID string
	ReasonRef           string
	RecordedAt          time.Time
}

// SupersedeOriginal marks original as superseded by successorIntentID and
// returns the one record of that link. It is the only change original
// undergoes: RequestState moves to SUPERSEDED (through [lifecycle.Machine]
// against this definition's own narrowed profile, exactly like every other
// transition in this package — never assigned directly), InstanceVersion and
// the two transition timestamps advance because a transition happened, and
// nothing else on original is read, written or recomputed. Every prior
// proposal revision, approval binding and evidence reference on original
// therefore survives byte-identical; [TestTodo_EP_INTENT_003_Mutation] proves
// it by diffing the encoded envelope field by field.
//
// A supersede is not a privilege escalation path: this function grants the
// successor nothing. The successor is a wholly separate [Instance] its
// caller mints the same way [CreateIntent] mints one — through this
// package's own admission (NewInstance/Draft), under the caller's own
// authorization — and this function is never given, and never needs, the
// successor's content to record that the link exists.
func SupersedeOriginal(original *Instance, def Definition, successorIntentID, reasonRef string, clock Clock) (SupersessionRecord, error) {
	if original == nil {
		return SupersessionRecord{}, fmt.Errorf("%w: instance is nil", ErrInvalidInstance)
	}
	if original.Definition != def.Ref {
		return SupersessionRecord{}, fmt.Errorf("%w: definition mismatch", ErrInvalidInstance)
	}
	if successorIntentID == "" {
		return SupersessionRecord{}, fmt.Errorf("%w: supersede requires a successor intent id", ErrInvalidInstance)
	}
	if successorIntentID == original.IntentID {
		return SupersessionRecord{}, ErrSupersedeSelf
	}
	if reasonRef == "" {
		return SupersessionRecord{}, fmt.Errorf("%w: supersede requires a reason_ref", ErrInvalidInstance)
	}

	switch original.Lifecycle.Request {
	case lifecycle.RequestCancelled, lifecycle.RequestSuperseded, lifecycle.RequestRejected,
		lifecycle.RequestWithdrawn, lifecycle.RequestClosed:
		return SupersessionRecord{}, fmt.Errorf("%w: request state is %s", ErrSupersedeAlreadyTerminal, original.Lifecycle.Request)
	}

	ctx := original.LifecycleContext(def)
	m, err := lifecycle.NewMachine(def.LifecycleProfiles(), original.Lifecycle, ctx)
	if err != nil {
		return SupersessionRecord{}, err
	}
	next := original.Lifecycle
	next.Request = lifecycle.RequestSuperseded
	now := clock()
	if err := m.Apply(next, ctx, lifecycle.TransitionRecord{
		At:        now,
		ReasonRef: reasonRef,
	}); err != nil {
		return SupersessionRecord{}, err
	}
	original.Lifecycle = m.Current()
	original.InstanceVersion++
	original.RecordedAt = now
	original.LastTransitionAt = now

	return SupersessionRecord{
		SupersededIntentID:  original.IntentID,
		SupersedingIntentID: successorIntentID,
		ReasonRef:           reasonRef,
		RecordedAt:          now.Time(),
	}, nil
}
