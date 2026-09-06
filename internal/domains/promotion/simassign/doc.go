// Package simassign simulates the Assignment and organization changes a
// promotion implies (PROMO-002).
//
// Semantic owner: People domain (composite ChangeRequest, promote_worker).
// Phase: P1A.
//
// What it is for. PROMO-001 produces one immutable
// [snapshot.PromotionInputSnapshot]: eight named business inputs, each bound to
// the revision, watermark, authority and disclosure decision it was read under.
// This package is the first half of the question that snapshot exists to
// answer -- "if this promotion happened, what would change in People, Org and
// Position?" -- and it answers it as a pure function of that snapshot plus the
// caller's stated intention. It reads nothing, writes nothing, reserves
// nothing and holds no clock.
//
// Three effects are proposed, and they are exactly the three People/Org/
// Position participants the promote-into-management reference workflow names
// inside its one ACID commit boundary
// (planning/reference-workflows/promote-into-management.md):
//
//   - [EffectAssignmentRevision] -- the new assignment revision: position, job,
//     grade, organizational unit and manager, effective-dated from the
//     snapshot's effective date, with the prior assignment closed the day
//     before ([Result.PriorAssignmentEnd]).
//   - [EffectManagerRelationship] -- the manager-relationship change and what
//     it does to the management chain.
//   - [EffectPositionOccupancy] -- the target position's occupancy transition,
//     carrying the POSITION-003 reservation the transition depends on.
//
// Five properties are deliberate.
//
// First, every effect derives from named snapshot inputs and says so.
// [ProposedEffect.DerivedFrom] lists the exact input names an effect was
// computed from, and [ProposedEffect.ExpectedRevision] is the watermark of the
// input that supplied its baseline. There is no field on [Request] through
// which a caller can supply a current value: the caller states the target
// placement and the proposed manager -- an intention -- and every "before" in
// every change comes from the snapshot. A snapshot whose digest does not
// recompute from its own material inputs is refused outright, so a hand-built
// snapshot carrying attacker-chosen current values cannot be simulated.
//
// Second, a WITHHELD input refuses the effect that needed it rather than
// producing a guess. The refusal is typed ([Refusal]), names the input and its
// availability, and carries no value -- the same rule the snapshot itself
// follows. An effect is never emitted with a placeholder, a zero or a default
// standing in for something authorization denied.
//
// Third, the management chain is evaluated, not asserted. A proposed manager
// who is the subject is a cycle and is refused. When the proposed manager
// already appears in the subject's resolved chain, the proposed chain is that
// chain from the proposed manager's level onward and its depth is checked
// against the declared bound. When the proposed manager appears nowhere in it,
// the snapshot does not establish their own chain, so the depth is reported
// UNKNOWN rather than assumed to be one: deciding it is PROMO-004's job with a
// second resolution, not this package's job with an assumption.
//
// Fourth, each effect carries its reversibility class and the compensation and
// observation references intent.CompilePlan requires, and declares whether it
// falls inside the local commit boundary. [Result.PlannedWrites] projects the
// changed fields onto intent.PlannedWrite; [Result.OutboxEffects],
// [Result.Compensations] and [Result.Observations] project the non-local ones
// onto the triple CompilePlan validates together. All three of this package's
// effects are local -- their inputs' authority class is NATIVE_STATE -- so a
// P1A ZERO_EFFECT promote_worker definition can compile a plan from them.
//
// Fifth, the result is one deterministic artifact with a canonical digest.
// Two simulations over byte-identical snapshots and identical intentions
// produce the same digest; any material change produces a new one.
package simassign
