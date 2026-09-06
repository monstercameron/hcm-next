// Package timer implements WF-RUN-004: durable workflow timers over the
// workflow_timer and workflow_ready_work tables migration 00026 materializes.
//
// # A timer is a promise, not a sleep
//
// A WAIT step does not sleep. It computes a wake condition -- internal/
// workflow/steps/wait's pure ComputeTimerRequirement does that from the
// compiled node's declared zone, tzdb release, business calendar and
// reference-update policy -- and this package writes that requirement down as
// a row: at fires_at, this node becomes ready. The row is the promise;
// [Scheduler.Fire] is a caller keeping it.
//
// # The dataset provenance is pinned by digest, not copied into columns
//
// WF-RUN-004's RED clause worries about a dataset revision silently changing
// an execution date. The defence here is that a timer's durable key is the
// wait requirement's own content digest, which covers the wake target, the
// zone and tzdb version, the calendar ref and version and the
// reference-update policy. A republished dataset produces a different
// requirement, and therefore a different digest, and therefore a different
// timer -- the old promise is still on the table under its old digest, and
// [Scheduler.Fire] refuses to settle a timer whose key does not match the
// requirement the caller now holds ([ErrRequirementDrift]). Nothing is
// recomputed behind the caller's back and no column is quietly rewritten.
//
// # Caller-driven, fenced, exactly once
//
// definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) blocks
// scheduler code, so there is no timer wheel, no ticker and no goroutine in
// this package: [Scheduler.Fire] is a function a caller invokes with its own
// clock reading, and it settles the timers that reading makes due. Every
// settle presents the caller's lease fence (internal/workflow/lease) and is
// verified against the durable lease before anything is written, so a worker
// whose lease was taken over cannot fire another worker's timers.
//
// Exactly-once is two compare-and-swaps rather than a promise about the
// caller: the timer row settles under its own version CAS, so a second
// concurrent Fire loses the race and settles nothing, and the ready-work row
// it produces is unique per (instance, node, attempt) with a derived id, so a
// replayed settle finds its own row rather than creating a second unit of
// work.
//
// # Misfire is SCHED-002's policy, applied here
//
// A timer that comes due long after its instant -- a process was down, a
// tenant was paused -- is a misfire, and what to do about it is a declared
// policy, not a default this package invents. [Decide] hands the timer's
// instant and the caller's instant to internal/engines/schedule's own
// ApplyMisfirePolicy and reports what it decided: ON_TIME and CATCH_UP settle
// at the timer's own instant, FIRE_NOW settles at the caller's, SKIP cancels
// the promise without waking anything, and REVIEW leaves the row pending for
// a human. A [FireRequest] with no declared policy is refused.
package timer
