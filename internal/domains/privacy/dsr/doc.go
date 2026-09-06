// Package dsr implements PRIV-005: intake and identity verification for a
// data-subject request (a "DSAR" -- access, rectification, erasure,
// restriction, portability or objection request a person files against
// their own data).
//
// Semantic owner: domain-teams, under internal/domains/privacy (a new
// subtree; PRIV-002's evidence lives separately, in
// internal/governance/privacy, because it governs notice/consent authority
// rather than a subject-initiated request). Phase: Gate C, per PRIV-005's
// todos.md entry. Depends on MODEL-027 (legal holds -- out of this
// package's scope; a held record still gets scheduled for fulfillment, it
// just cannot be destroyed), TRUST-002 (federation-verified principals,
// out of scope) and WORK-001 (immutable WorkItem lifecycle, out of scope;
// PRIV-006 is expected to route a verified, advanceable request onto a
// WorkItem for fulfillment) only in the sense that a real caller wires this
// package's evidence into those; this package does not import any of them.
//
// # Two steps, not one
//
// [Intake] records a claimed request immutably: who the requester says they
// are ([SubjectClaims], never yet trusted), which [Kind] of request they
// are making, which [legal.Jurisdiction] governs it, when and through which
// [Channel] it arrived. Nothing here is authorized yet -- Intake only
// establishes the record and its statutory clock (see below).
//
// [DataSubjectRequest.Verify] is the separate, later step that binds an
// identity-assurance [IdentityEvidence] reference to the request. It
// refuses to advance the request past VerificationUnverified unless that
// evidence meets [AssuranceFloor] for the request's Kind -- erasure demands
// the highest floor this package declares, because it is the one request
// kind whose fulfillment is irreversible.
//
// [DataSubjectRequest.CanAdvance] is the gate a downstream fulfillment
// capability (PRIV-006) is expected to call before doing anything with a
// request. It re-derives its answer from the request's own stored fields
// every time -- verification state, duplicate linkage, and assurance
// against the declared floor -- rather than trusting a cached boolean, so a
// request that was somehow assembled with VerificationVerified set but an
// assurance level below its Kind's floor (a forged or mis-migrated record,
// not just one that went through [DataSubjectRequest.Verify] correctly)
// still cannot advance.
//
// # The statutory clock is declared data, not prose
//
// [ClockTable] maps (jurisdiction, kind) to a response-deadline day count.
// [Intake] resolves a request's [DataSubjectRequest.Deadline] by looking
// the pair up in the table supplied to it -- falling back from an exact
// jurisdiction match to state-only, country-only, and finally the
// zero-value default entry every kind must declare (see
// [DefaultClockTable] and the PRIV-005 CONFORMANCE test) -- rather than any
// switch statement naming a specific statute. Extending coverage to a new
// jurisdiction is a data change to the table, never a code change to this
// package.
//
// # Duplicates are linked, not duplicated
//
// [DetectDuplicate] matches a candidate request against previously-recorded
// ones sharing the same tenant, the same [SubjectClaims.Key], the same
// [Kind], and a received-at instant within a caller-supplied window.
// [Intake] records every request it is asked to record -- it never refuses
// a duplicate submission outright -- but sets
// [DataSubjectRequest.DuplicateOf] to the earliest matching request's ID,
// and [DataSubjectRequest.CanAdvance] refuses to advance a linked
// duplicate: only the primary request in a window is ever executable.
package dsr
