// Package reconcile implements RECON-001: a durable reconciliation-job
// lifecycle for committed mandatory effects
// (planning/todos.md `RECON-001`; specs/transaction-ledger-reconciliation-and-repair.md;
// specs/integration-platform.md).
//
// # What a reconciliation job is
//
// internal/effectgraph compiles committed effects, and every effect whose
// ObservationContract declares Required=true has promised that its external
// result will be checked, not merely attempted. A [Job] is that promise made
// durable: one row per (effect, comparison policy) naming what was intended,
// what the external system is now known to say, how fresh an observation has
// to be before it counts, how many times a caller has looked, when to look
// again, when to give up, who is currently responsible, and the resting or
// terminal [Status] that resulted.
//
// # What this package does not do
//
// It does not compare an observation against policy to reach a business
// completion verdict -- that is RECON-002's [Comparer] port, not yet
// implemented, and this package only calls it. It does not read an external
// system -- that is the [Observer] port, backed by internal/connectivity/observe.
// It does not repair anything. [Coordinator.Trigger] and [Coordinator.Poll]
// coordinate those two ports and the job's own durable state through [Store];
// neither method mutates domain state or a provider directly, and a
// conformance test in this package's own test suite scans its non-test source
// for imports outside a declared allowlist to keep that true.
//
// # Fencing and idempotency
//
// Every write this package makes presents a caller's internal/workflow/lease
// [lease.Fence], verified against the durable lease before anything is
// written -- the same discipline internal/workflow/timer and
// internal/workflow/lease itself use, and for the same reason: a worker whose
// lease was taken over while it was away must not resume a job it no longer
// owns. [JobID] is derived from (tenant, effect, policy), so a duplicate
// trigger for the same effect and comparison policy addresses the row that
// already exists ([Triggered.Replay]) rather than creating a parallel one.
//
// # Never disappearing
//
// A stale, partial or otherwise insufficiently fresh observation is still an
// attempt, but it never ends polling on its own: [Coordinator.Poll] only asks
// the [Comparer] for a verdict once an observation meets the job's declared
// [Status] is [StatusObserving] or [StatusUnknown] until then, both resting
// states that stay due. A job that reaches its deadline without a terminal
// comparison verdict is exhausted, not lost: it settles to [StatusExpired] or
// [StatusRepairRequired] (chosen from the committed effect's own repair
// policy, carried on the job since it was triggered), and the row stays as
// its own evidence forever -- this package never deletes a job.
//
// # Clock through a port
//
// Nothing in this package reads the wall clock. Every method that needs an
// instant takes it from the caller ([TriggerRequest.Now], [PollRequest.Now]),
// exactly as internal/workflow/timer and internal/workflow/lease already do,
// so a scheduler-shaped background loop is never smuggled in behind this
// package's own back.
package reconcile
