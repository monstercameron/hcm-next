// Package recover implements WF-RUN-003: recovering one node execution after
// the worker that held it died.
//
// # What "died" means here
//
// Nothing in this package decides that a worker is dead. A worker is dead
// exactly when the lease it held on its node execution has lapsed by the
// caller's own clock reading, which is what [Recoverer.Inspect] observes and
// nothing else asserts. definitions/runtime/durable-runtime-decision.yaml
// (WF-RUN-000) blocks scheduler code, so there is no reaper here, no
// heartbeat monitor, no goroutine and no ambient clock: the instant comes
// from the [Clock] port on every call, and a caller that wants recovery run
// on a schedule runs it on its own schedule.
//
// # The two dispositions
//
// A dead attempt has exactly two honest shapes, and they are told apart by
// the durable idempotency record TX-006 keeps, never by guessing:
//
//   - The effect was already recorded. Its record is COMPLETED under the
//     attempt's idempotency key, so the effect happened; the new attempt
//     reads the stored [idempotency.ResultIdentity] and advances on the
//     outcome [Effect.Replay] reconstructs from it, without running the
//     effect a second time ([DispositionReplayResult]).
//   - Nothing was recorded. The effect never committed, so the new attempt
//     performs it -- under the same idempotency key, which is why a late
//     duplicate from the dead worker that comes back is either replayed
//     (same digest) or refused with IDEMPOTENCY_CONFLICT (different digest)
//     rather than dispatched twice ([DispositionExecuteEffect]).
//
// The key is attempt-independent on purpose ([AttemptKey]): an attempt
// number in the key would make every recovery a fresh namespace, and the
// first thing recovery would then do is duplicate the effect it was supposed
// to protect. TestTodo_WF_RUN_003_Mutation is exactly that mutation.
//
// # Three transactions, four persistence boundaries
//
// One recovery is three caller-owned transactions, in this order:
//
//	TX1  take over the lapsed lease, mint the next fence, retire the dead
//	     attempt (RUNNING -> FAILED -> RETRYING) and schedule the next one
//	TX2  dispatch the business effect under the fence and the TX-006 guard,
//	     or replay the stored result identity when one is already recorded
//	TX3  advance the node under the same fence (runtime.AdvanceFenced),
//	     dispatching every derived continuation, and release the lease
//
// The gaps between them are the four persistence boundaries WF-RUN-003's
// REFACTOR clause asks for, and each is a declared [Phase] a [Failpoint] can
// crash at deterministically: [PhaseBeforeNodeStateCommit] (TX1 rolls back),
// [PhaseAfterNodeStateCommitBeforeDispatch] (the attempt is scheduled and
// nothing has been dispatched), [PhaseAfterDispatchBeforeResultCommit] (the
// effect and its idempotency record are durable, the advancement is not) and
// [PhaseAfterResultCommit] (everything is durable). A crash at any of them
// leaves the next [Recoverer.Recover] call able to finish the work from the
// durable rows alone, and none of them can produce a second business effect.
//
// # Two guards, two halves
//
// The fence and the idempotency key are not redundant, and the mutation test
// separates them. The fence ([runtime.FenceVerifier], verified before TX2
// dispatches anything and again by [runtime.AdvanceFenced] before TX3 reads
// any workflow state) refuses the superseded holder before it reaches the
// effect at all; the idempotency key is what makes the effect itself
// exactly-once for whoever does reach it. Remove the key and the recoverer
// duplicates the effect it was recovering; remove the fence and a superseded
// holder's dispatch is admitted all the way to the guard, where the key is
// then the only thing left standing between it and a second effect.
//
// # What this package does not own
//
// It performs no business effect of its own: [Effect] is a port, and its
// implementation owns the ledger append, the outbox row and whatever else
// the capability actually does. It stores no runtime state of its own
// either -- every row it writes belongs to internal/workflow/runtime,
// internal/workflow/lease or internal/transaction/idempotency, and this
// package adds no table and no migration.
package recover
