// Package scheduler is SVC-004's runtime: the durable dispatch loop
// cmd/scheduler runs.
//
// # What it does
//
// One tick, per (tenant, queue) claim it was configured with:
//
//  1. takes or renews that queue's lease through [lease.Manager], which mints
//     the epoch fence timer firing presents;
//  2. if it is the queue's holder, settles every due workflow_timer under that
//     fence (internal/workflow/timer's Fire), which is what turns a promise
//     into a durable workflow_ready_work row;
//  3. returns abandoned work to the pool -- a workflow_ready_work row left
//     DISPATCHED by a replica whose instance lease has since lapsed goes back
//     to READY, which is why a restart loses nothing;
//  4. selects the admissible, eligible ready work in one declared order,
//     claims each unit by taking that instance's own lease and moving the row
//     READY -> DISPATCHED under its compare-and-swap version, and
//  5. hands each claim to a [Dispatcher] outside the claiming transaction,
//     then settles the row DONE, CANCELLED or back to READY.
//
// # Admission and fairness
//
// Each (tenant, queue) claim is bounded by [Config.BatchSize]. Within that
// bound, the scheduler admits at most [Config.BulkBatchShare] rows per tick
// whose priority is [BulkPriority] or a less urgent value, plus every higher-
// priority row. REPLAY-mode instances are always treated as background for
// this bound and are ordered below EXECUTE-mode work even if a caller gives a
// replay row an accidentally urgent numeric priority. Rows held back remain
// READY for the next tick; no work is discarded and one tenant's claim budget
// cannot be spent on another tenant because every selection is tenant-scoped.
//
// # Why two replicas never publish the same logical execution
//
// Three independent mechanisms, each durable:
//
//   - the selection statement takes a row lock with SKIP LOCKED, so two
//     replicas scanning at the same instant partition the batch instead of
//     fighting over it;
//   - a claim acquires the WORKFLOW_INSTANCE lease before it moves the row, so
//     one instance is being advanced by at most one replica -- a second
//     replica gets [lease.ErrHeld] and leaves the row alone;
//   - the row move itself is a compare-and-swap on ready_version, so even a
//     caller that reached past both of the above writes nothing.
//
// Only step 2 is gated on holding the queue lease. Steps 3 to 5 are not, so a
// replica that lost the queue race still drains the frontier alongside the
// holder rather than idling -- it is safe to, because those steps carry their
// own refusals.
//
// The fence the claim carries ([Work.Fence]) is the instance lease's, so the
// dispatcher advances the instance under exactly the epoch this scheduler
// took -- a replica that stalled and came back is refused by comparison
// (WF-RUN-002), not by hoping it noticed.
//
// # What it deliberately does not do
//
// No workflow semantics. This package never resolves a plan, never computes a
// node outcome and never decides what a WAIT node means; it moves durable rows
// and calls [Dispatcher]. The adapter that turns one claim into an
// internal/workflow/execute Driver call is a composition root's job, because
// building an execute.ResumeTimerRequest needs the pinned plan and the typed
// step resolution, which are workflow semantics.
//
// It also creates no state of its own: every table it touches
// (workflow_ready_work, workflow_timer, workflow_lease, workflow_instance)
// belongs to internal/data/runtimestate and migration 00026/00016. Kill the
// process at any point and the next tick resumes from those rows.
//
// # Identifiers
//
// Nothing here names github.com/google/uuid.
// definitions/architecture/dependency-roles.yaml reserves that direct import
// to internal/kernel, internal/intent, internal/ledger, internal/data,
// internal/connectivity, internal/transaction, internal/humanwork,
// internal/workflow, internal/operations/explorer,
// internal/engines/wire/digest, cmd and test -- internal/platform is not one
// of them, and tools/policy/libfirewall enforces it. Every identifier this
// package handles therefore travels inside a value some owning package
// already typed: a [lease.AcquireRequest] carries the tenant, a
// [runtimestate.ReadyWork] carries the work, a [lease.Fence] carries the
// resource. That is also why [Config.Claims] is a slice of acquire requests
// rather than a list of tenant ids: the composition root (cmd/scheduler,
// which may import uuid) decides which tenants a replica serves.
package scheduler
