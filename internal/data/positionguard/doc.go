// Package positionguard is PROMOUX-004's reservation-ownership guard: the
// one database-enforced admission control that lets exactly one proposal
// hold a target position for a given effective date.
//
// # Why this package exists rather than a check in application code
//
// internal/domains/promotion.evaluateTargetPositionSelection can prove a
// target position exists, is vacant, is compatible with a proposal's
// desired job and organization, and is effective at the proposed date --
// every one of those is a fresh read, never a caller assertion. None of
// that proves the caller is entitled to actually take the slot right now: a
// second, concurrent proposal could pass all four of those same checks
// against the same position and date microseconds later, and whichever one
// reads first would not know about the other. Moving that check into
// application code narrows the race window but cannot close it -- see
// internal/data/promotionguard's package doc for the full argument, which
// applies here unchanged. [Admit] is what actually closes it.
//
// # The guarantee, precisely
//
// migrations/00287 declares
// promotion_target_position_guard_one_active_window, a partial UNIQUE index
// on (tenant_id, position_ref, effective_date) WHERE status = 'ACTIVE'.
// [Admit] never performs a bare SELECT to decide whether to insert: it
// issues one INSERT .. ON CONFLICT .. DO UPDATE .. RETURNING statement that
// is itself the admission decision. PostgreSQL evaluates the unique index
// as part of that statement's own commit, so of two genuinely concurrent
// Admit calls for the same (tenant, position, effective date), the database
// -- not this package's control flow -- decides which one's INSERT lands
// first.
//
// # Replays versus conflicts
//
// A caller retrying its own request presents the same idempotency key it
// used the first time. [Admit] treats that as a replay: the ON CONFLICT DO
// UPDATE clause matches only when the conflicting row's idempotency_key
// equals the caller's own, and when it matches, [Admit] reports the
// existing reservation rather than refusing. A different, later proposal
// for the same position and date presents a different idempotency key; the
// DO UPDATE's WHERE clause then matches nothing, no row is touched, and
// [Admit] reports [ErrActiveConflict].
//
// # Adapter
//
// [Adapter] is the concrete internal/domains/promotion.PositionReservationAdmitter
// this package supplies: it opens its own tenant-scoped transaction, calls
// [Admit], and translates a conflict into a plain "not admitted" boolean
// rather than an error, because a reservation conflict is a business
// refusal (a Finding, in the domain's vocabulary), not a contract failure.
package positionguard
