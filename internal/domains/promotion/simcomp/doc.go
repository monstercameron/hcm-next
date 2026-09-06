// Package simcomp simulates the compensation and budget effects a promotion
// implies (PROMO-003).
//
// Semantic owner: Rewards domain (composite ChangeRequest, promote_worker).
// Phase: P1A.
//
// What it is for. It is the second half of the question PROMO-001's immutable
// [snapshot.PromotionInputSnapshot] exists to answer -- the first half,
// Assignment and organization, is internal/domains/promotion/simassign -- and
// like that half it is a pure function of the snapshot plus the caller's stated
// intention. It reads nothing, writes nothing, reserves nothing and holds no
// clock. In particular it never calls budget.ReservationStore.Reserve: a
// simulation that took a hold would be an effect, so what it produces is the
// exact budget.CompensationReservationRequest that would be issued, validated
// and left unissued.
//
// Two effects are proposed, and they are the two Rewards/Finance participants
// the promote-into-management reference workflow names inside its commit
// boundary (planning/reference-workflows/promote-into-management.md):
//
//   - [EffectCompensationRevision] -- the base-pay change as a proposed
//     compensation revision, in exact decimal, on the COMP-002 annualized
//     basis the snapshot's own band-position inputs were computed on.
//   - [EffectBudgetReservation] -- the BUDGET-002 reservation delta against
//     the compensation pool the snapshot observed.
//
// Four properties are deliberate.
//
// First, both money values come from the snapshot, never from the caller. The
// current and desired annualized amounts are read out of the two COMP-003
// band-position inputs the snapshot already bound, so the arithmetic is done on
// the same annualization the band placement was decided on rather than on a
// second one this package would be free to compute differently. [Request]
// carries no amount field at all, and a snapshot whose digest does not
// recompute from its own material inputs is refused outright.
//
// Second, the pay-band position of the desired pay is reported as a typed
// [BandFinding] with a BELOW/WITHIN/ABOVE class, the boundary it sits against
// and the approval that class triggers -- the compensation partner below the
// band, the finance partner above it -- rather than as a boolean "in band".
//
// Third, an input the caller was not authorized to see refuses the effect that
// needed it. A withheld current pay produces a typed [Refusal] naming the
// input, not a base-pay change computed from zero; a withheld or absent pool
// observation refuses the reservation rather than assuming funds. An
// insufficient pool is likewise a refusal, and it states the shortfall in the
// same currency as the pool.
//
// Fourth, proration is stated, not conventional. The caller declares the pay
// period and the days-per-year divisor; the result carries the day counts on
// each side of the effective date, both daily rates, both portions and the
// period delta, all in exact decimal at the declared scale and rounding mode.
// Nothing here reaches for a conventional constant that would silently move
// every number the day somebody changed the convention.
//
// The effect vocabulary -- reversibility classes, effect kinds, field changes,
// proposed effects and refusals -- is declared once, in simassign, and reused
// here, so PROMO-004 can take the union of the two effect sets without
// translating between two vocabularies that would then be free to drift.
package simcomp
