// Package intentcontrol is the data-plane store over migration 00024's
// BusinessIntent, proposal, decision and transaction control tables (owner:
// data plane; DB-011).
//
// # What it stores, and what it deliberately does not
//
// Migration 00004 gave the kernel two tables: intent_instance (the five
// lifecycle dimensions and the canonical request digest) and proposal_revision
// (an immutable revision row carrying one digest). Everything that explains
// those two rows -- the origin, causation, correlation, family and mode an
// intent was created under, the change request it realizes, the input snapshot
// a preflight read, the simulation result it produced, the proposal's own
// write/effect/approval/obligation sets, the relationships between intents,
// results, decisions, certificates, and the whole transaction plan, commit
// receipt, abort receipt, ambiguity record, correction, repair plan and
// closure chain -- had no table. Migration 00024 adds them; this package is the
// only Go code that writes and reads them.
//
// It is a store, not a policy engine. It never decides whether an approval is
// sufficient, whether a plan may commit or whether a correction is warranted:
// those decisions belong to internal/intent and the transaction coordinator.
// What this package guarantees is that a decision, once made, is recorded in a
// shape that cannot later be quietly changed.
//
// # The four refusals that are Go code rather than schema
//
// Most of DB-011's RED clause is answered by the schema itself (see migration
// 00024's header). Four invariants are graph- or cross-table properties that no
// single-row constraint can see, so they live here, each executed inside the
// caller's own transaction so that the check and the write cannot be separated
// by a concurrent writer:
//
//  1. [RelationshipStore.Link] walks the ancestor chain before inserting an
//     edge and refuses a cycle longer than one hop ([ErrRelationshipCycle]).
//     The one-hop case (parent = child) is already refused by the schema.
//  2. [ReceiptStore.RecordCommit] and [ReceiptStore.RecordAbort] each refuse
//     when the counterpart receipt already exists ([ErrReceiptConflict]): a
//     plan that both committed and aborted is a contradiction two independent
//     UNIQUE constraints cannot catch between them. Because that check spans
//     two tables, a transaction-scoped advisory lock per (tenant, plan) makes
//     the check and the insert one critical section, so two concurrent
//     coordinators cannot each read "no counterpart" and each write one.
//  3. [SimulationStore.RecordResult] refuses a result whose input snapshot
//     belongs to a different intent ([ErrSnapshotMismatch]). The foreign key
//     proves the snapshot exists in the tenant; it does not prove it is this
//     intent's snapshot.
//  4. Every compare-and-swap update ([ChangeRequestStore.Transition],
//     [PlanBindingStore.Transition], [AmbiguityStore.Resolve],
//     [RepairPlanStore.Transition]) reports [ErrVersionConflict] when the
//     caller's expected version is not the stored one, and writes nothing.
//
// # Tenancy
//
// Every table migration 00024 creates is row-level-security protected and
// keyed on the app.tenant_id session setting. Every method here therefore
// takes its [Executor] explicitly rather than holding a handle: the caller has
// to have scoped its transaction with internal/data/tenancy.WithTenant before
// any statement runs, and a store that opened its own connection would make
// that impossible to guarantee. This is the same shape internal/data/workforce
// and internal/humanwork/workitem already use.
//
// # Wiring that is not this package's to do
//
// The intent write path (internal/intent/app and its pgstore) is what should
// populate intent_instance_context, the proposal set tables and the closure row
// in the same transaction as the intent_instance and proposal_revision rows it
// already writes. That wiring is outside this lane's file roots; this package
// implements and proves the stores, and the exact call sites another lane must
// add are named in DB-011's report.
package intentcontrol
