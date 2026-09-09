package recover

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Executor is the minimal database capability this package's read paths need.
// It is exactly [runtime.Executor], restated so a caller does not have to name
// the runtime package to call [Recoverer.Inspect].
type Executor = runtime.Executor

// Beginner opens the three transactions one recovery is made of. A pool or a
// dedicated connection satisfies it. This package never holds a transaction
// across a call it returns from: each of TX1, TX2 and TX3 is begun, used and
// finished inside [Recoverer.Recover].
type Beginner interface {
	Begin(ctx context.Context) (dbport.Tx, error)
}

// Clock is the caller's own reading of the current instant, as a port.
//
// Nothing in this package reads a wall clock. Expiry is an observation made
// against the instant this port supplies, which is what lets a test decide
// that a worker died without waiting for a real lease to lapse -- and what
// stops this package from being able to disagree with itself about "now"
// between the lease check and the write that depends on it.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to [Clock].
type ClockFunc func() time.Time

// Now implements [Clock].
func (f ClockFunc) Now() time.Time { return f() }

var _ Clock = ClockFunc(nil)

// FixedClock is a [Clock] that always reports the same instant. It is the
// honest way to say "this whole recovery happened at one stated time"
// without a test having to thread a closure through every call.
type FixedClock time.Time

// Now implements [Clock].
func (c FixedClock) Now() time.Time { return time.Time(c) }

var _ Clock = FixedClock{}

// EffectRequest is everything the [Effect] port is told about the attempt
// whose business effect it is being asked to perform or to explain.
//
// Attempt is the *new* attempt number the recovery scheduled, and it is
// deliberately absent from both Scope and Digest: the effect is the same
// semantic effect it would have been on the attempt that died, and an
// attempt number folded into either one would make every recovery a fresh
// idempotency namespace.
type EffectRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Attempt    int

	// Scope and Digest are the TX-006 coordinates the effect is guarded
	// under. An implementation may record them, and must not vary its own
	// behavior with Attempt.
	Scope  idempotency.Scope
	Digest string

	// Fence is the lease fence the recovering worker holds. It is supplied so
	// an implementation can stamp its own evidence with the fence token that
	// authorized it; it has already been verified before this call.
	Fence runtime.Fence

	// CorrelationID ties the effect to the run that caused it.
	CorrelationID string
	// RecordedAt is the recovery's own instant, from the [Clock] port.
	RecordedAt time.Time
}

// EffectResult is what one performed effect reports: the identity TX-006
// stores for replay, and the node outcome the advancement runs on.
type EffectResult struct {
	// Identity is stored verbatim on the idempotency record and is the only
	// thing a later replay is given to work from, so it must reference
	// everything a replay needs to reconstruct Outcome.
	Identity idempotency.ResultIdentity
	// Outcome is the typed node outcome runtime.Advance is applied with.
	Outcome frontier.NodeOutcome
}

// Effect is the business effect one node attempt performs, and the only
// thing in a recovery that is not this package's own bookkeeping.
//
// The two methods are the two dispositions WF-RUN-003 names, and an
// implementation must keep them consistent: whatever Perform returns as its
// [idempotency.ResultIdentity] is the only input Replay is given, so an
// identity that cannot be turned back into the outcome it produced is an
// identity that makes the effect unrecoverable.
type Effect interface {
	// Perform dispatches the effect exactly once, inside tx, and reports what
	// to store and what to advance on. It runs inside TX-006's guard: it is
	// called only when this call won the reservation, and everything it
	// writes commits atomically with the record that says it happened.
	//
	// It must have no side channel outside tx. An implementation that calls
	// an external system directly gives up the exactly-once property this
	// package is built to provide, because a rolled-back transaction cannot
	// un-send an HTTP request.
	Perform(ctx context.Context, tx dbport.Tx, req EffectRequest) (EffectResult, error)

	// Replay reconstructs the outcome a previously committed effect produced,
	// from the stored result identity and the durable rows it references. It
	// performs no effect and writes nothing.
	Replay(ctx context.Context, ex Executor, req EffectRequest, stored idempotency.ResultIdentity) (frontier.NodeOutcome, error)
}

// EffectOutcome is what [Recoverer.DispatchEffect] settled on for one
// attempt.
type EffectOutcome struct {
	// Outcome is the node outcome to advance on, either freshly produced by
	// [Effect.Perform] or reconstructed by [Effect.Replay].
	Outcome frontier.NodeOutcome
	// Identity is the stored result identity, in both cases read back from
	// the durable idempotency record rather than from memory.
	Identity idempotency.ResultIdentity
	// Replayed reports that the effect was already recorded and was not run
	// again. It is the observable difference between WF-RUN-003's two
	// dispositions.
	Replayed bool
}
