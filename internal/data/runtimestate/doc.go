// Package runtimestate holds DB-012's cross-store conformance suite.
//
// # What this is
//
// planning/todos.md's DB-012 asks for one thing three other packages already
// each proved independently: that the durable workflow-runtime and
// human-work tables -- migration 00016 (workflow_instance,
// workflow_node_execution), 00017 (work_item, work_item_transition), 00018
// (workflow_continuation), 00019 (idempotency_record) and 00020
// (workflow_advancement_receipt) -- compose correctly when a real caller uses
// more than one of them inside a single transaction. WF-RUN-001/023/025
// (internal/workflow/runtime), WORK-001/002/003 (internal/humanwork/workitem)
// and TX-006 (internal/transaction/idempotency) each already have their own
// conformance suite for their own store in isolation; this package adds no
// new table and reimplements none of that -- it composes their already
// exported ports over one pgtest database and proves the properties that only
// show up at the seam: a shared caller transaction commits or rolls back
// every store together, a continuation dispatched twice does not double the
// governed work it creates, and a process that reconnects after a restart can
// resume purely from what the database, not any of the three packages' own
// memory, still holds.
//
// # What this deliberately is not
//
// definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) records BUILD
// for the in-house Postgres runtime and, in the same breath, a blocking
// re-evaluation gate in front of every scheduler, lease, fencing, timer and
// signal-subscription primitive. This package therefore never asserts that a
// lease, timer, checkpoint, child-link, queue or SLA table exists: it asserts
// the opposite -- [SchemaInventory] and the tests built on it check that the
// live schema carries none of them, and that the gate record on disk still
// names the same blocking condition, rather than the test simply assuming an
// absence that could have quietly become a stale assumption. GREEN's mention
// of "approval requirement/decision tables" is not tested as an absence here
// for the same reason: WORK-001's own REFACTOR clause already explains that
// omission (an approval is a work_item specialization, not a second table),
// which is an architectural decision internal/humanwork/workitem's own suite
// documents, not a WF-RUN-000 gate this package needs to re-prove.
//
// # A finding this suite surfaces rather than silently working around
//
// internal/workflow/runtime/advancement_receipt.go defines
// recordAdvancementReceipt and loadAdvancementReceipt -- the functions that
// would write and replay workflow_advancement_receipt rows -- but
// internal/workflow/runtime/advance.go's own Advance never calls either one.
// The table and its migration exist; the write path that WF-RUN-025's
// "receipt" vocabulary implies does not yet run in production code. Because
// internal/workflow/runtime is out of this package's scope to edit, the
// advancement-receipt exactly-once test here proves the table's own schema
// contract directly (its primary key and its version-advances CHECK), rather
// than through runtime.Advance, and says so at the point it does it. Wiring
// Advance itself to call these two functions is follow-up work for the
// workflow-runtime lane, not this ticket.
package runtimestate
