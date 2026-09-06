// Package lease implements WF-RUN-002: execution leases and fencing over the
// durable workflow_lease table migration 00026 materializes.
//
// # What a lease is here
//
// A lease is an exclusive, expiring claim on one runtime resource -- a
// workflow instance, a node execution, a work item or a queue -- held by one
// workload identity. Taking it mints a fence token that is monotonic across
// the whole history of that resource, not merely across its live holders.
// The token, not the lease's liveness, is the safety property: a holder that
// stalled, was superseded and then came back cannot tell by looking at itself
// that it lost the resource, but it can be refused by comparison, because the
// token it still carries is behind the one the table now holds.
//
// Every write a holder performs therefore presents its [Fence], and every
// fenced write path verifies it first: [Manager.Verify] reads the live lease
// under a row lock and refuses a token that is behind ([ErrFenceStale]), a
// token that names a different lease line ([ErrFenceForeign], which also
// unwraps to [ErrFenceStale]) or a resource nobody holds any more
// ([ErrLeaseLost]). internal/workflow/runtime's AdvanceFenced is the
// advancement-shaped caller of exactly that check, wired to it by [Fenced].
//
// # No reaper, no heartbeat goroutine
//
// definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) blocks
// scheduler code, and a lease reaper is scheduler code: a background loop
// that decides other workers are dead. So nothing in this package runs on its
// own. Expiry is observed, not swept -- a caller reads the live lease with
// [Manager.Observe] against its own clock reading, and if it has lapsed the
// caller retires it with [Manager.Expire] in its own transaction, or simply
// takes it with [Manager.Acquire], which retires the lapsed row and mints the
// next token in the same caller transaction. [Manager.Renew] is the holder's
// own heartbeat, called by the holder, on the holder's schedule. There is no
// ticker, no goroutine and no ambient clock anywhere in this package: every
// method that needs an instant takes it as an argument.
//
// # Identity
//
// WF-RUN-002's REFACTOR clause says the lease owner is a workload identity
// rather than a process hostname alone, so [Identity] is two parts -- the
// workload the code belongs to and the replica running it -- and refuses a
// bare hostname. holder_id in the table is their canonical join, so an
// operator reading a stuck lease sees which workload as well as which
// process.
//
// # Evidence
//
// Every transition returns an [Evidence] record with a content digest:
// ACQUIRED, TAKEN_OVER, RENEWED, RELEASED, EXPIRED, VERIFIED and REFUSED.
// The evidence is derived, never a second authority -- [Manager.History]
// reconstructs the same records from the durable workflow_lease rows alone,
// which is what makes an evidence record checkable rather than merely
// reported.
package lease
