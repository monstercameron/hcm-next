// Package privacy implements the PRIV-002 evidence chain: prove that an
// optional (consent-governed) processing purpose was actually authorized,
// not merely that a notice exists somewhere.
//
// Semantic owner: governance-and-trust. Phase: P1A/P1B, per the 2026-09-02
// disposition in planning/todos.md ("Consent/notice: Pilot notice evidence
// and optional processing authority if used"). Todo: PRIV-002. Depends on
// PRIV-001 (the processing-activity inventory, out of this lane's scope),
// MODEL-027 (legal holds, out of scope) and MSG-004 (deterministic
// templates, out of scope) only in the sense that a real caller would wire
// this package's evidence into those; this package does not import them.
//
// # The three records
//
// [Notice] is one versioned, jurisdiction-scoped privacy notice: what
// purpose it covers, which data classes it discloses, and the exact content
// this version presents (its [Notice.Digest]). A notice by itself
// authorizes nothing.
//
// [Presentation] is the evidence that one exact notice version was actually
// shown to one principal, in one locale, through one channel that met this
// package's accessibility bar, at one recorded time. It binds the notice's
// own digest, not just its version string, so a notice republished under an
// unchanged version number can never stand in for what a principal actually
// saw.
//
// [OptionalProcessing] is one principal's consent decision for one
// consent-governed purpose: its scope, which presentation it was granted
// against, and its expiry/withdrawal state. Withdrawal never deletes or
// mutates the original grant; [OptionalProcessing.Withdraw] returns a new
// record, because the withdrawal transition is itself evidence.
//
// # Authority is deny-by-default
//
// [EvaluateAuthority] is the one function that turns a Notice, a
// Presentation and an OptionalProcessing into a yes/no. Every missing,
// stale, inaccessible, out-of-scope, expired or withdrawn piece of evidence
// denies; nothing here defaults to allow. A withdrawn consent specifically
// returns [AuthorityProcessingBlocked] with restrict/delete obligations
// attached, per PRIV-002's GREEN clause.
//
// A [Notice] with [Notice.Mandatory] set can never be authorized through
// this function, regardless of what Presentation or OptionalProcessing
// evidence accompanies it: mandatory legal basis (tax reporting, statutory
// recordkeeping, and the like) is governed by internal/governance/legal and
// PRIV-001, not by consent. This is PRIV-002's REFACTOR invariant
// ("mandatory legal basis and optional consent are never conflated"),
// enforced structurally rather than left to caller discipline.
//
// [AuthorityDecision] is deliberately shaped like
// [github.com/monstercameron/hcm-next/internal/trust/authz.Decision]: an
// explainable yes/no with a uniform reason code, the obligations it
// attaches, and a canonical digest and evidence id over every input it was
// computed from. It is not that type reused verbatim -- authz's Decision
// carries tenant/scope/field concepts this package has no reason to import
// -- but the same discipline: an evidence-bearing record a durable store can
// point to, not a bare boolean.
package privacy
