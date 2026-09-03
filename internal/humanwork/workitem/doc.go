// Package workitem owns the durable WorkItem: the unit of human responsibility
// the workflow frontier's WORK_ITEM_REQUIRED intent creates, and the evidence
// of everything that ever happened to it.
//
// Semantic owner: human work (BI.WORK). Phase: P1B. Todos: WORK-001, WORK-002,
// WORK-003.
//
// # Two tables, one of them append-only
//
// migrations/00017_work_item.sql declares the split this package is built on:
//
//	work_item             where responsibility is now  (transitions in place)
//	work_item_transition  how it got there             (append-only evidence)
//
// Every write goes through one code path, [Store.advance], which updates the
// item under an optimistic item_version compare-and-swap and appends the
// matching transition row in the same statement pair. The transition table
// carries UNIQUE (tenant_id, work_item_id, item_version), so a gap or a fork in
// the evidence chain is a constraint violation rather than a missing audit
// line. This package never opens a transaction of its own: pass a [dbport.Tx]
// the caller began and will finish, because on a bare connection the item
// update and its evidence row would commit independently and a crash between
// them would leave a state change nothing explains.
//
// # The lifecycle
//
//	CREATED -> ROUTED -> ASSIGNED  -> CLAIMED -> IN_PROGRESS -> COMPLETED
//	                  -> AVAILABLE                           -> RETURNED
//	                  -> ESCALATED
//	   (from any non-terminal state: ESCALATED, EXPIRED, CANCELLED)
//
// [LegalTransition] is that diagram, read literally, and it is checked in Go
// before any statement runs. Two edges deserve a note. CLAIMED and IN_PROGRESS
// may go back to ASSIGNED or AVAILABLE: that is the claim-expiry return
// described below, not a demotion. RETURNED and ESCALATED may go back to
// ROUTED: a returned or escalated item is re-resolved by policy, which is what
// keeps it from being stranded.
//
// The vocabulary is exactly the eleven statuses in [Statuses], and the CHECK
// constraint in migration 00017 lists the same eleven. A status like OPEN or
// PENDING is refused: WORK-001's RED clause names a generic status as a defect
// because a status that does not say what is happening cannot be routed on,
// escalated on, or explained.
//
// planning/specs/human-work-forms-and-rules.md additionally draws
// WAITING_INPUT hanging off IN_PROGRESS. It is deliberately not declared here:
// no operation in this phase's port reaches it, and a status nothing can
// produce is a status a projection would have to explain and a caller could
// never see. It is added when the operation that produces it is.
//
// # An approval is a specialization, not a second store
//
// [KindApproval] plus [WorkItem.ApprovalRequirementRef] is the whole of the
// ApprovalTask specialization, and migration 00017 requires the two to agree:
// an APPROVAL item must name the requirement it decides and a TASK must not
// carry one. There is no parallel approval-task table, which is WORK-001's
// REFACTOR clause. [NewApprovalTask] builds one from a compiled
// [humanwork.ApprovalRequirement].
//
// # Assignment records; it does not authorize
//
// [ResolveAssignment] runs the existing resolver — [humanwork.Resolve] over a
// compiled [humanwork.ApprovalRequirement] and a [humanwork.Directory] — and
// records what it decided: the resolution expression and its digest, the
// surviving candidate set, every exclusion with the rule id that produced it,
// the chosen owner, the directory and governance-policy versions the answer
// came from, and the trigger that made this resolution run. Nothing about
// candidate selection is reimplemented here.
//
// Recording an owner grants that owner nothing. WORK-002's REFACTOR clause is
// that assignment does not itself confer decision authority, and this package
// honours it by having no authority field at all: [Assignment.IsCandidate]
// answers "was this principal in the set when it was resolved", which is
// evidence, and authority at decision time is re-established by resolving
// again against the directory as it is then. An assignment that resolved to
// nobody does not strand the item — it places it ESCALATED, with the whole
// exclusion list attached.
//
// # The claim, and why it is not a lease
//
// definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) gates every
// scheduler, lease, fencing and timer primitive behind a blocking P1B
// re-evaluation. So WORK-003's "fenced lease" is implemented here without any
// of them. There is no scheduler, no background worker, no lease-expiry
// sweeper, no timer and no retry in this package, and there is no clock: every
// instant is supplied by the caller.
//
// What there is instead:
//
//   - an exclusive claim recorded on the item row itself — claim id, holder,
//     claimed-at and a caller-supplied expiry — won by the item_version
//     compare-and-swap. Exactly one UPDATE can match a given version, so
//     exactly one claimant wins; every loser is refused [CodeAlreadyClaimed]
//     and mutates nothing. A claimant holding a stale version commits nothing
//     at all.
//   - expiry evaluated only when a caller touches the item, never swept. A
//     touch that finds a claim expired against the caller's own `Now` releases
//     it and returns the item to its policy route — ASSIGNED when the route
//     named a principal, AVAILABLE when it named a candidate set — recording
//     that release as its own transition. [Store.Claim] then applies the new
//     claim in the same transaction. [Store.Start], [Store.Complete] and
//     [Store.Return] release the expired claim and refuse the operation with
//     [CodeClaimExpired]; the returned item is the released one, and a caller
//     that commits keeps the release. Rolling back instead loses nothing,
//     because expiry is a property of the row and of `Now` and the next touch
//     rediscovers it.
//
// [Store.Load] is a read and is therefore not a touch: it reports the stored
// claim as it stands and leaves the row alone. Ask [WorkItem.ClaimExpired] if
// you want the verdict without taking it.
//
// # Tenant scoping
//
// Every row carries tenant_id explicitly and migration 00017 puts the same
// fail-closed row level security policy on both tables that
// migrations/00016_workflow_runtime.sql puts on the runtime tables. A caller
// that wants the policy enforced sets app.tenant_id the way
// internal/data/tenancy.WithTenant does; this package never infers a tenant
// from a session, and a cross-tenant read is [CodeWorkItemNotFound] rather
// than a disclosure that the item exists.
package workitem
