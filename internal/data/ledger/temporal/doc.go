// Package temporal implements the ledger's bitemporal query and state
// reconstruction APIs (owner: data plane; phase: P1A; LEDGER-006).
//
// It is read-only. It appends nothing, owns no table and creates no
// migration: every answer here is derived from ledger_event exactly as
// LEDGER-001/LEDGER-004 recorded it, read under an authorization decision
// the caller has already evaluated.
//
// # The six modes
//
// Five of them - CURRENT, EFFECTIVE_AS_OF, KNOWN_AS_OF, BETWEEN and HISTORY -
// are DATA-005's, and this package delegates them to
// internal/data/bitemporal rather than re-deriving the same SQL a second
// time. What this package adds on top of that adapter is what the ledger
// itself owes a caller and a projection cannot be trusted to supply:
//
//   - RECONSTRUCT, the sixth mode: the whole state of one subject at one
//     bitemporal coordinate, resolved per field, with superseded assertions
//     removed and unpromoted evidence kept strictly separate (see below).
//   - An authority label on every returned assertion, resolved from
//     authority_assignment: not just the authority_ref string the event
//     carries, but the assignment's kind, domain scope and half-open
//     business interval, plus whether that interval actually covers the
//     assertion's own effective instant.
//   - The source event ID on every answer, so any value this package
//     returns can be traced back to the exact immutable event it came from.
//
// # Why an external observation can never appear as domain truth
//
// The ledger records five assertion classes and only two of them bear
// authority (internal/data/ledger.AssertionClass.RequiresAuthority). An
// EXTERNAL_OBSERVATION is what some other configured authority reported; a
// CLAIM is an assertion not yet promoted. Neither is a fact this platform
// asserts, and the ledger specification is explicit that recording an
// assertion never implies authority over its subject
// (specs/transaction-ledger-reconciliation-and-repair.md 8.5).
//
// A naive "latest wins per field" resolution destroys that distinction:
// an external observation recorded after a domain fact, for the same
// schema_ref and a later effective instant, would simply win and be
// returned as the current value. This package therefore never resolves
// across truth classes. [Fold] partitions the visible assertions by
// [TruthClass] first and only then picks a winner inside each partition, so
// [State.Domain] can only ever hold assertions whose origin is a DOMAIN_FACT
// and [State.Unpromoted] is returned as a separate, unresolved evidence list
// that no caller can mistake for state.
//
// A CORRECTION inherits the truth class of the assertion it ultimately
// corrects, walked back through the corrects_stream_key/corrects_sequence
// chain ([resolveTruthClass]). A correction whose origin cannot be resolved
// inside the visible set - because the target is not visible at this
// coordinate, is not authorized for this caller, or does not exist - is
// classified [TruthUnresolved] and lands in Unpromoted. Failing closed here
// is deliberate: an unresolved correction must never be promoted into
// domain truth on the strength of a target nobody proved.
//
// # Half-open boundaries
//
// Both temporal bounds are half-open in the same direction the rest of the
// platform uses. BETWEEN's window is [EffectiveFrom, EffectiveTo), so a fact
// effective exactly at a window boundary belongs to exactly one of two
// adjacent windows and can never be returned by both. A point coordinate is
// inclusive on both axes (effective_at <= EffectiveAt, recorded_at <=
// KnownAt), which is what "as of this instant" means; the authority
// assignment interval that labels an assertion is half-open the same way the
// appender already requires (internal/data/ledger.Appender.checkAuthority),
// so an assignment ending exactly at an assertion's effective instant does
// not cover it.
//
// # Plans and answer equivalence
//
// A reconstruction has more than one legitimate query plan. [LedgerPlan]
// pushes the coordinate and the authorization decision into one SQL
// statement and folds the rows it gets back. [HistoryFoldPlan] instead pages
// internal/data/bitemporal's own HISTORY mode and folds the result in Go.
// A future projection-backed plan is a third implementation of the same
// [Plan] interface.
//
// Every plan must produce the same answer, and [VerifyPlanEquivalence] is
// how that is checked rather than assumed: it runs each plan over the same
// request and decision and compares [State.Digest], reporting the exact
// plans that disagree. That is what makes it safe to serve a reconstruction
// from anything other than the ledger itself - the faster plan is only ever
// permitted because its answer is provably the ledger-derived answer.
package temporal
