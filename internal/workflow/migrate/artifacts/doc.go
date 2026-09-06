// Package artifacts migrates the pending runtime artifacts a live workflow
// instance holds outside its own row onto the epoch a WF-RUN-018 migration
// just moved that instance to (WF-RUN-026).
//
// # What "artifact" means here
//
// internal/workflow/migrate moves an instance's pinned workflow version,
// compiled-plan digest and frontier. It does not move the separately
// persisted state that instance is waiting on, because that state lives in
// other tables owned by other packages: a timer promise
// (internal/workflow/timer), an open signal subscription, a ready-work row, a
// routed approval work item (internal/humanwork/workitem), a child link, and
// the lease its owner is holding it under. Each of those is a pending
// artifact. Left behind, they either strand (the instance now sits at a node
// nothing will ever wake) or duplicate (the new epoch raises a second promise
// for a wait the old epoch already made). [Migrate] is what moves them.
//
// # One contract, six handlers, one canonical receipt
//
// Every artifact kind is migrated by a handler, and every handler answers the
// same versioned contract ([ContractVersion]): given the instance's [Scope]
// -- the epoch it came from, the epoch it is going to, the fence its owner
// holds and the caller's instant -- produce the canonical [Entry] list saying
// what happened to each artifact of that kind. The handlers run in the fixed
// order [Handlers] declares, lease first, because migrating artifacts under a
// lease somebody else holds is not a migration but a theft. Their entries are
// collected into one digested [Receipt].
//
// A handler may only reach four verdicts, and [Disposition] names them all:
// the artifact is carried unchanged, re-keyed onto the new epoch, found
// already present under the new epoch (so nothing is written a second time),
// or -- as an error, never a silent verdict -- refused.
//
// # What is preserved
//
// Semantic identity, owner and deadline. A timer that promised to wake an
// instance at a particular instant still wakes it at exactly that instant
// after the migration: re-keying a timer changes the durable key (the wake
// requirement's digest, which is what WF-RUN-004 keys a timer on) and the
// node, and nothing else. A requirement whose FireAt is not bit-identical to
// the promise on the table is refused as [CodeWakeInstantMoved] rather than
// applied. An approval keeps its owner, its deadline and its own work-item
// identity. A subscription keeps its signal name and correlation key. Ready
// work keeps its eligibility instant, priority and attempt.
//
// # What is refused
//
// A stale lease, always ([CodeStaleLease]). A fence that names a token the
// resource has moved past, a lease that lapsed by the caller's own instant, a
// live lease with no fence presented at all: every one of those refuses the
// whole run. A stale lease is never carried forward under a new epoch.
//
// And any artifact this package cannot relocate faithfully
// ([CodeNotRelocatable]). Two kinds are deliberately in that set. A routed
// approval work item's node is immutable through internal/humanwork/
// workitem's own exported surface, and minting a second work item would mint
// a second unit of human responsibility for one decision. A child link's
// parent node is likewise immutable, and an awaited child whose parent node
// moved would have nowhere to report back to. Both refuse rather than
// approximate.
//
// # Atomicity, and what a partial failure leaves behind
//
// [Migrate] writes only through the caller's transaction -- the same one
// [migrate.Migrate] ran in. A refusal at any handler, or a failure injected
// between two of them through [Barrier], returns an error having committed
// nothing: the caller rolls back and the instance is left exactly as it was,
// PAUSED on its old version and runnable. A caller that cannot roll back --
// because it has already committed the version move -- records the failure
// durably instead with [MarkRepairRequired], which walks the instance to
// REPAIR_REQUIRED along a legal path of internal/workflow/runtime's own state
// machine. Either way, no continuation is duplicated: every write this
// package makes addresses a derived identity and is a no-op when that
// identity already exists.
//
// # No clock, no goroutine
//
// Like every package under internal/workflow/runtime, this one reads no wall
// clock and starts nothing. [Scope.MigratedAt] is the caller's own instant,
// and it is the only "now" that exists here.
package artifacts
