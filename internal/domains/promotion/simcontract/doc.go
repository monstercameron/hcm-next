// Package simcontract assembles the full WorkflowSimulationContract for the
// promote-into-management vertical slice (PROMO-004).
//
// Semantic owner: People/Rewards composite (promotion). Phase: P1A.
//
// A WorkflowSimulationContract is the one immutable artifact that binds
// together everything a Promotion (or a Manager Change, which reuses the same
// shape per PROMO-006) simulation decided: which intent and input snapshot it
// was computed from, the digest of the exact candidate proposal it describes,
// and twelve declared sections -- reads, writes, streams, conflicts,
// approvals, authority, legal obligations, side effects, cost, completion,
// revalidation and repair. [Validate] refuses a contract missing any one of
// them, naming the missing section rather than failing generically, because a
// reviewer who cannot tell *which* section is absent cannot fix it.
//
// This package is kernel-pure: it computes nothing about a promotion or a
// manager change on its own. It composes results a caller already produced --
// PROMO-002/003's simassign and simcomp effect proposals, GOVERN-002's
// governance vocabulary, CONFLICT-001/002's write-footprint candidates,
// INTG-001's external-operation boundary, and the kernel's own
// internal/intent shapes -- into one digestable, once-persistable record.
// Every side effect it carries is stamped [EffectSimulatedNotExecuted] by
// construction: there is no exported way to mark one executed, which is what
// [TestTodo_PROMO_004_Security] proves.
//
// [Assemble] is the only constructor. It never touches a domain revision
// store, an external operation port, the outbox, a MessageIntent sink or a
// WorkItem store -- [TestTodo_PROMO_004] proves that with fakes of each that
// fail the test if they are ever called.
package simcontract
