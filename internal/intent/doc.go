// Package intent is the Human Capital Management Suite business-intent kernel.
//
// Semantic owner: intent-and-capability. Phase: P1A.
//
// The kernel owns four things and nothing else:
//
//   - Identity and definition. Three kernel families — CHANGE_REQUEST,
//     CALCULATION_REQUEST and ANALYTICAL_REQUEST — and the compiled-in
//     [Registry] of the drafted [Definition]s, resolved by (intent_type_id,
//     version) rather than by display name.
//   - Envelope. The typed [Instance] that records what is wanted, by whom, for
//     which subjects, with a server-derived principal reference, an idempotency
//     key and a canonical request digest.
//   - Proposal. Immutable [ProposalRevision] artifacts whose material digest is
//     what an approval binds. Control snapshots are revalidated context, not
//     material: republishing a policy bundle must not invalidate every pending
//     approval.
//   - Plan. A non-executable [TransactionPlan] value object. P1A compiles
//     plans and never runs them; there is no Execute anywhere in this package.
//
// The kernel owns no domain facts. Domain knowledge reaches it through ports:
// [Preflighter] for domain preflight, [Digester] for canonical digests, and the
// snapshot inputs ([GovernanceSnapshot], [ConflictSnapshot], [BaselineSnapshot])
// that a governance engine, conflict engine or domain projection fills in.
//
// Lifecycle lives in the lifecycle subpackage; the mapping to and from the
// generated Protobuf contracts lives in the protomap subpackage. Neither this
// package nor its subpackages import internal/data, internal/transport or any
// concrete adapter.
package intent
